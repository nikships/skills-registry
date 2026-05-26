"""Tests for the remote server assembly + env-var validation."""

from __future__ import annotations

from pathlib import Path

import pytest
from cryptography.fernet import Fernet
from cryptography.hazmat.backends import default_backend
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa

from skills_mcp.remote_server import (
	ServerSettings,
	build_server,
	build_storage,
	load_settings,
)


@pytest.fixture(scope="module")
def rsa_pem() -> str:
	key = rsa.generate_private_key(public_exponent=65537, key_size=2048, backend=default_backend())
	return key.private_bytes(
		encoding=serialization.Encoding.PEM,
		format=serialization.PrivateFormat.PKCS8,
		encryption_algorithm=serialization.NoEncryption(),
	).decode("ascii")


@pytest.fixture
def env(monkeypatch: pytest.MonkeyPatch, tmp_path: Path, rsa_pem: str) -> dict[str, str]:
	values = {
		"FASTMCP_SERVER_AUTH_GITHUB_BASE_URL": "https://mcp.example.com",
		"FASTMCP_SERVER_AUTH_GITHUB_CLIENT_ID": "ov23liabcdef",
		"FASTMCP_SERVER_AUTH_GITHUB_CLIENT_SECRET": "secret",
		"GITHUB_APP_ID": "3846201",
		"GITHUB_APP_PRIVATE_KEY": rsa_pem,
		"GITHUB_APP_WEBHOOK_SECRET": "whsec",
		"GITHUB_APP_SLUG": "skills-registry-mcp",
		"JWT_SIGNING_KEY": "x" * 88,
		"FASTMCP_STORAGE_DIR": str(tmp_path / "oauth"),
		"STORAGE_ENCRYPTION_KEY": Fernet.generate_key().decode("ascii"),
		"PORT": "9000",
		"HOST": "127.0.0.1",
	}
	# Wipe any pre-set vars first so the tests start from a clean slate.
	for k in values:
		monkeypatch.delenv(k, raising=False)
	for k, v in values.items():
		monkeypatch.setenv(k, v)
	return values


def test_load_settings_validates_each_var(
	env: dict[str, str], monkeypatch: pytest.MonkeyPatch
) -> None:
	# Sanity: with everything present we get a settings object back.
	settings = load_settings()
	assert settings.base_url == "https://mcp.example.com"
	assert settings.port == 9000
	assert settings.host == "127.0.0.1"
	assert settings.install_url == ("https://github.com/apps/skills-registry-mcp/installations/new")
	assert settings.mcp_url == "https://mcp.example.com/mcp"


@pytest.mark.parametrize(
	"missing",
	[
		"FASTMCP_SERVER_AUTH_GITHUB_BASE_URL",
		"FASTMCP_SERVER_AUTH_GITHUB_CLIENT_ID",
		"FASTMCP_SERVER_AUTH_GITHUB_CLIENT_SECRET",
		"GITHUB_APP_ID",
		"GITHUB_APP_PRIVATE_KEY",
		"GITHUB_APP_WEBHOOK_SECRET",
		"GITHUB_APP_SLUG",
		"JWT_SIGNING_KEY",
		"FASTMCP_STORAGE_DIR",
		"STORAGE_ENCRYPTION_KEY",
	],
)
def test_load_settings_fails_fast_on_missing(
	env: dict[str, str], monkeypatch: pytest.MonkeyPatch, missing: str
) -> None:
	monkeypatch.delenv(missing, raising=False)
	with pytest.raises(EnvironmentError) as exc:
		load_settings()
	assert missing in str(exc.value)


def test_load_settings_creates_storage_dir(env: dict[str, str], tmp_path: Path) -> None:
	target = tmp_path / "oauth"
	assert not target.exists()
	load_settings()
	assert target.is_dir()


def test_build_storage_returns_fernet_wrapper(env: dict[str, str]) -> None:
	settings = load_settings()
	storage = build_storage(settings)
	# We don't introspect the wrapper's private state; calling put/get would
	# require asyncio. Confirm we got back the right wrapper class.
	from key_value.aio.wrappers.encryption import FernetEncryptionWrapper

	assert isinstance(storage, FernetEncryptionWrapper)


async def test_build_server_wires_tools_and_routes(env: dict[str, str]) -> None:
	settings = load_settings()
	server, link_store, app_client = build_server(settings)
	# Tools registered.
	tools = await server.list_tools()
	tool_names = {t.name for t in tools}
	assert tool_names == {"search_skills", "get_skill"}
	# Link store is a real LinkStore.
	from skills_mcp.linking import LinkStore as ExpectedLinkStore

	assert isinstance(link_store, ExpectedLinkStore)
	# App client carries our creds.
	assert app_client._creds.app_id == "3846201"


async def test_build_server_registers_production_middleware(env: dict[str, str]) -> None:
	"""The production middleware stack is registered in the documented order.

	The order matters: error handling outermost, rate limiting next,
	structured logging innermost (see ``skills_mcp.middleware``). A
	silent reorder would tank either auditability or rate-limit safety,
	so we pin the contract here.
	"""
	from fastmcp.server.middleware.error_handling import ErrorHandlingMiddleware
	from fastmcp.server.middleware.logging import StructuredLoggingMiddleware
	from fastmcp.server.middleware.rate_limiting import RateLimitingMiddleware

	settings = load_settings()
	server, _, _ = build_server(settings)
	# FastMCP exposes the configured middleware via the ``middleware``
	# attribute on the server instance.
	mws = list(server.middleware)
	assert isinstance(mws[0], ErrorHandlingMiddleware)
	assert isinstance(mws[1], RateLimitingMiddleware)
	assert isinstance(mws[2], StructuredLoggingMiddleware)


def test_build_server_masks_error_details(env: dict[str, str]) -> None:
	"""``mask_error_details=True`` keeps raw GitHub errors out of MCP responses.

	The flag is stored on a private attribute in v3; we read it via that
	name. This is a deliberate seam: if FastMCP renames or removes the
	attribute we want this test to fail loudly so we can update the
	wiring rather than ship a silently-broken safeguard.
	"""
	settings = load_settings()
	server, _, _ = build_server(settings)
	assert server._mask_error_details is True


def test_server_settings_install_url_uses_slug() -> None:
	settings = ServerSettings(
		base_url="https://x",
		github_client_id="x",
		github_client_secret="x",
		github_app_id="1",
		github_app_private_key="-----BEGIN PRIVATE KEY-----\nx\n-----END PRIVATE KEY-----",
		github_app_webhook_secret="x",
		github_app_slug="my-app",
		jwt_signing_key="x",
		storage_dir=Path("/tmp/x"),
		storage_encryption_key="x",
		host="0.0.0.0",
		port=8000,
	)
	assert settings.install_url == "https://github.com/apps/my-app/installations/new"
	assert settings.mcp_url == "https://x/mcp"
