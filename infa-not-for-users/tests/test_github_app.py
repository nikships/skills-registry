"""Tests for the GitHub App JWT + installation-token client."""

from __future__ import annotations

import time
from collections.abc import Callable
from typing import Any

import httpx
import jwt
import pytest
from cryptography.hazmat.backends import default_backend
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa

from skills_mcp.github_app import (
	GitHubAppClient,
	GitHubAppCredentials,
	GitHubAppError,
	with_retry,
)


@pytest.fixture(scope="module")
def rsa_pem() -> str:
	"""A real 2048-bit RSA keypair PEM so PyJWT can sign + verify."""
	key = rsa.generate_private_key(public_exponent=65537, key_size=2048, backend=default_backend())
	return key.private_bytes(
		encoding=serialization.Encoding.PEM,
		format=serialization.PrivateFormat.PKCS8,
		encryption_algorithm=serialization.NoEncryption(),
	).decode("ascii")


@pytest.fixture
def creds(rsa_pem: str) -> GitHubAppCredentials:
	return GitHubAppCredentials(app_id="123456", private_key_pem=rsa_pem)


def test_credentials_reject_empty_app_id(rsa_pem: str) -> None:
	with pytest.raises(ValueError, match="App ID"):
		GitHubAppCredentials(app_id="  ", private_key_pem=rsa_pem)


def test_credentials_reject_non_pem_key() -> None:
	with pytest.raises(ValueError, match="PEM"):
		GitHubAppCredentials(app_id="1", private_key_pem="not a key")


def test_mint_app_jwt_has_required_claims(creds: GitHubAppCredentials, rsa_pem: str) -> None:
	client = GitHubAppClient(creds)
	now = int(time.time())
	token = client.mint_app_jwt(now=now)

	# Decode without sig verify first to peek at claims; then verify.
	decoded = jwt.decode(
		token,
		_public_pem(rsa_pem),
		algorithms=["RS256"],
		options={"verify_aud": False},
	)
	assert decoded["iss"] == "123456"
	# iat is backdated by 60s per GitHub's recommendation.
	assert decoded["iat"] == now - 60
	# exp is 9 minutes out.
	assert decoded["exp"] == now + 9 * 60


def test_mint_app_jwt_handles_clock_skew_request(creds: GitHubAppCredentials) -> None:
	client = GitHubAppClient(creds)
	# Without ``now``, uses time.time(). Just confirm we get a non-empty string.
	assert isinstance(client.mint_app_jwt(), str) is True


async def test_mint_installation_token_success(
	creds: GitHubAppCredentials,
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	captured: dict[str, Any] = {}

	def handler(request: httpx.Request) -> httpx.Response:
		captured["url"] = str(request.url)
		captured["auth"] = request.headers.get("Authorization", "")
		captured["accept"] = request.headers.get("Accept", "")
		captured["api_version"] = request.headers.get("X-GitHub-Api-Version", "")
		return httpx.Response(201, json={"token": "ghs_installtoken123", "expires_at": "..."})

	_install_mock_transport(monkeypatch, handler)
	client = GitHubAppClient(creds)
	token = await client.mint_installation_token(99)

	assert token == "ghs_installtoken123"
	assert captured["url"] == "https://api.github.com/app/installations/99/access_tokens"
	assert captured["auth"].startswith("Bearer ")
	assert captured["accept"] == "application/vnd.github+json"
	assert captured["api_version"] == "2022-11-28"


async def test_mint_installation_token_404_raises(
	creds: GitHubAppCredentials,
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	_install_mock_transport(monkeypatch, lambda req: httpx.Response(404, text="missing"))
	client = GitHubAppClient(creds)
	with pytest.raises(GitHubAppError) as exc:
		await client.mint_installation_token(7)
	assert exc.value.status == 404


async def test_list_installation_repos_paginates(
	creds: GitHubAppCredentials,
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	pages = {
		"1": _repos_page([f"user/repo{i}" for i in range(100)]),
		"2": _repos_page(["user/repo100", "user/repo101"]),
	}

	def handler(request: httpx.Request) -> httpx.Response:
		page = request.url.params.get("page", "1")
		return httpx.Response(200, json=pages[page])

	_install_mock_transport(monkeypatch, handler)
	client = GitHubAppClient(creds)
	repos = await client.list_installation_repos("token")

	assert len(repos) == 102
	assert repos[0].full_name == "user/repo0"
	assert repos[-1].full_name == "user/repo101"
	assert all(r.default_branch == "main" for r in repos)


async def test_list_installation_repos_single_page(
	creds: GitHubAppCredentials,
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	def handler(request: httpx.Request) -> httpx.Response:
		return httpx.Response(200, json=_repos_page(["acme/skills"]))

	_install_mock_transport(monkeypatch, handler)
	client = GitHubAppClient(creds)
	repos = await client.list_installation_repos("token")
	assert [r.full_name for r in repos] == ["acme/skills"]


async def test_with_retry_retries_then_succeeds() -> None:
	calls = {"n": 0}

	async def factory() -> str:
		calls["n"] += 1
		if calls["n"] == 1:
			raise GitHubAppError("503", status=503)
		return "ok"

	result = await with_retry(factory, attempts=3, base_delay_s=0.0)
	assert result == "ok"
	assert calls["n"] == 2


async def test_with_retry_re_raises_non_retryable() -> None:
	async def factory() -> None:
		raise GitHubAppError("404", status=404)

	with pytest.raises(GitHubAppError) as exc:
		await with_retry(factory, attempts=3, base_delay_s=0.0)
	assert exc.value.status == 404


# ------------------------------------------------------------ helpers


def _install_mock_transport(
	monkeypatch: pytest.MonkeyPatch,
	handler: Callable[[httpx.Request], httpx.Response],
) -> None:
	"""Replace httpx.AsyncClient with one that uses a MockTransport.

	The real client is constructed inside the production code via plain
	``httpx.AsyncClient(...)``, so the simplest swap is to patch the class
	to inject our transport every time.
	"""
	real = httpx.AsyncClient

	def fake(*args: Any, **kwargs: Any) -> httpx.AsyncClient:
		kwargs["transport"] = httpx.MockTransport(handler)
		return real(*args, **kwargs)

	monkeypatch.setattr(httpx, "AsyncClient", fake)


def _public_pem(private_pem: str) -> bytes:
	private = serialization.load_pem_private_key(
		private_pem.encode("ascii"),
		password=None,
		backend=default_backend(),
	)
	return private.public_key().public_bytes(
		encoding=serialization.Encoding.PEM,
		format=serialization.PublicFormat.SubjectPublicKeyInfo,
	)


def _repos_page(full_names: list[str]) -> dict[str, Any]:
	return {
		"total_count": len(full_names),
		"repositories": [{"full_name": fn, "default_branch": "main"} for fn in full_names],
	}
