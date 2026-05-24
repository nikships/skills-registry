"""Tests for the /github/webhook handler."""

from __future__ import annotations

import hashlib
import hmac
import json
from collections.abc import Callable
from typing import Any
from unittest.mock import AsyncMock

import httpx
import pytest
from key_value.aio.stores.memory import MemoryStore
from starlette.requests import Request

from skills_mcp.github_app import GitHubAppClient, GitHubAppCredentials, InstallationRepo
from skills_mcp.linking import LinkedRepo, LinkStore
from skills_mcp.webhooks import WebhookHandler


@pytest.fixture
def secret() -> str:
	return "very-secret-string"


@pytest.fixture
def link_store() -> LinkStore:
	return LinkStore(MemoryStore())


@pytest.fixture
def app_client_stub() -> GitHubAppClient:
	# A throwaway client; real network calls are replaced via AsyncMock in tests.
	return GitHubAppClient(
		GitHubAppCredentials(app_id="1", private_key_pem=_DUMMY_PEM),
	)


def test_handler_rejects_empty_secret(
	app_client_stub: GitHubAppClient, link_store: LinkStore
) -> None:
	with pytest.raises(ValueError, match="secret"):
		WebhookHandler(secret="", app_client=app_client_stub, link_store=link_store)


async def test_webhook_rejects_bad_signature(
	secret: str, app_client_stub: GitHubAppClient, link_store: LinkStore
) -> None:
	handler = WebhookHandler(secret=secret, app_client=app_client_stub, link_store=link_store)
	body = b'{"action":"created"}'
	request = _request(body, signature="sha256=wrong", event="installation")
	response = await handler(request)
	assert response.status_code == 401


async def test_webhook_accepts_ignored_event(
	secret: str, app_client_stub: GitHubAppClient, link_store: LinkStore
) -> None:
	handler = WebhookHandler(secret=secret, app_client=app_client_stub, link_store=link_store)
	body = b'{"action":"ping"}'
	request = _request(body, signature=_sig(secret, body), event="ping")
	response = await handler(request)
	assert response.status_code == 200
	assert b"ignored" in response.body


async def test_installation_created_links_repo_with_skills(
	secret: str,
	app_client_stub: GitHubAppClient,
	link_store: LinkStore,
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	monkeypatch.setattr(
		app_client_stub,
		"mint_installation_token",
		AsyncMock(return_value="ghs_install_42"),
	)
	monkeypatch.setattr(
		app_client_stub,
		"list_installation_repos",
		AsyncMock(return_value=[InstallationRepo("alice/skills", "main")]),
	)
	# `repo_has_skills` makes a separate HTTP call; stub via httpx MockTransport.
	_install_mock_transport(
		monkeypatch,
		_handler(
			{
				"https://api.github.com/repos/alice/skills/contents/": [
					{"name": "foo", "type": "dir"}
				],
				"https://api.github.com/repos/alice/skills/contents/foo/SKILL.md": {
					"encoding": "base64",
					"content": "LS0tCm5hbWU6IEZvbwotLS0KaGV5Cg==",  # ---\nname: Foo\n---\nhey
				},
			}
		),
	)

	handler = WebhookHandler(secret=secret, app_client=app_client_stub, link_store=link_store)
	body = json.dumps(
		{
			"action": "created",
			"installation": {"id": 42, "account": {"id": 1001}},
		}
	).encode("utf-8")
	request = _request(body, signature=_sig(secret, body), event="installation")

	response = await handler(request)
	assert response.status_code == 200
	link = await link_store.get_link("1001")
	assert link == LinkedRepo(installation_id=42, repo="alice/skills", default_branch="main")


async def test_installation_deleted_clears_link(
	secret: str,
	app_client_stub: GitHubAppClient,
	link_store: LinkStore,
) -> None:
	await link_store.set_link("u-9", LinkedRepo(99, "a/b", "main"))
	handler = WebhookHandler(secret=secret, app_client=app_client_stub, link_store=link_store)
	body = json.dumps(
		{
			"action": "deleted",
			"installation": {"id": 99, "account": {"id": 9}},
		}
	).encode("utf-8")
	request = _request(body, signature=_sig(secret, body), event="installation")

	response = await handler(request)
	assert response.status_code == 200
	assert await link_store.get_link("u-9") is None
	assert await link_store.user_for_installation(99) is None


async def test_installation_created_picks_skills_named_repo_when_multiple(
	secret: str,
	app_client_stub: GitHubAppClient,
	link_store: LinkStore,
	monkeypatch: pytest.MonkeyPatch,
) -> None:
	monkeypatch.setattr(
		app_client_stub,
		"mint_installation_token",
		AsyncMock(return_value="ghs"),
	)
	monkeypatch.setattr(
		app_client_stub,
		"list_installation_repos",
		AsyncMock(
			return_value=[
				InstallationRepo("alice/notes", "main"),
				InstallationRepo("alice/skills", "main"),
				InstallationRepo("alice/blog", "main"),
			]
		),
	)
	# All three repos have valid SKILL.md content — selection should still
	# favour the one literally named `skills`.
	skill_blob = {
		"encoding": "base64",
		"content": "LS0tCm5hbWU6IFgKLS0tCg==",
	}
	_install_mock_transport(
		monkeypatch,
		_handler(
			{
				"https://api.github.com/repos/alice/notes/contents/": [
					{"name": "foo", "type": "dir"}
				],
				"https://api.github.com/repos/alice/notes/contents/foo/SKILL.md": skill_blob,
				"https://api.github.com/repos/alice/skills/contents/": [
					{"name": "bar", "type": "dir"}
				],
				"https://api.github.com/repos/alice/skills/contents/bar/SKILL.md": skill_blob,
				"https://api.github.com/repos/alice/blog/contents/": [
					{"name": "baz", "type": "dir"}
				],
				"https://api.github.com/repos/alice/blog/contents/baz/SKILL.md": skill_blob,
			}
		),
	)

	handler = WebhookHandler(secret=secret, app_client=app_client_stub, link_store=link_store)
	body = json.dumps(
		{
			"action": "created",
			"installation": {"id": 11, "account": {"id": 222}},
		}
	).encode("utf-8")
	request = _request(body, signature=_sig(secret, body), event="installation")
	await handler(request)

	link = await link_store.get_link("222")
	assert link is not None
	assert link.repo == "alice/skills"


# ------------------------------------------------------------ helpers


_DUMMY_PEM = "-----BEGIN RSA PRIVATE KEY-----\nfake\n-----END RSA PRIVATE KEY-----\n"


def _sig(secret: str, body: bytes) -> str:
	return "sha256=" + hmac.new(secret.encode("utf-8"), body, hashlib.sha256).hexdigest()


def _request(body: bytes, *, signature: str, event: str) -> Request:
	"""Build a minimal Starlette Request that .body() will return ``body`` for."""

	async def receive() -> dict[str, Any]:
		return {"type": "http.request", "body": body, "more_body": False}

	scope = {
		"type": "http",
		"method": "POST",
		"path": "/github/webhook",
		"raw_path": b"/github/webhook",
		"query_string": b"",
		"root_path": "",
		"headers": [
			(b"x-hub-signature-256", signature.encode("ascii")),
			(b"x-github-event", event.encode("ascii")),
			(b"content-type", b"application/json"),
		],
	}
	return Request(scope, receive)  # type: ignore[arg-type]


def _install_mock_transport(
	monkeypatch: pytest.MonkeyPatch,
	handler: Callable[[httpx.Request], httpx.Response],
) -> None:
	real = httpx.AsyncClient

	def fake(*args: Any, **kwargs: Any) -> httpx.AsyncClient:
		kwargs["transport"] = httpx.MockTransport(handler)
		return real(*args, **kwargs)

	monkeypatch.setattr(httpx, "AsyncClient", fake)


def _handler(responses: dict[str, Any]) -> Callable[[httpx.Request], httpx.Response]:
	def _inner(request: httpx.Request) -> httpx.Response:
		key = str(request.url).split("?", 1)[0]
		body = responses.get(key)
		if body is None:
			return httpx.Response(404, text=f"unmocked: {key}")
		return httpx.Response(200, json=body)

	return _inner
