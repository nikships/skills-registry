"""Tests for the unauthenticated /healthz + landing + install-callback routes."""

from __future__ import annotations

from typing import Any

import pytest
from starlette.requests import Request

from skills_mcp.setup_routes import make_routes


@pytest.fixture
def routes() -> dict[str, Any]:
	return make_routes(
		install_url="https://github.com/apps/foo/installations/new",
		mcp_url="https://mcp.example.com/mcp",
	)


async def test_healthz_returns_ok(routes: dict[str, Any]) -> None:
	response = await routes["/healthz"](_get_request("/healthz"))
	assert response.status_code == 200
	assert response.body == b'{"status":"ok"}'


async def test_landing_renders_install_and_mcp_url(routes: dict[str, Any]) -> None:
	response = await routes["/"](_get_request("/"))
	assert response.status_code == 200
	assert b"https://github.com/apps/foo/installations/new" in response.body
	assert b"https://mcp.example.com/mcp" in response.body


async def test_install_callback_echoes_install_id(routes: dict[str, Any]) -> None:
	response = await routes["/github/app/callback"](
		_get_request("/github/app/callback?installation_id=42")
	)
	assert response.status_code == 200
	assert b"installation 42" in response.body


async def test_install_callback_handles_missing_install_id(routes: dict[str, Any]) -> None:
	response = await routes["/github/app/callback"](_get_request("/github/app/callback"))
	assert response.status_code == 200
	assert b"unknown" in response.body


def _get_request(target: str) -> Request:
	path, _, query = target.partition("?")
	scope = {
		"type": "http",
		"method": "GET",
		"path": path,
		"raw_path": path.encode("ascii"),
		"query_string": query.encode("ascii"),
		"root_path": "",
		"headers": [],
	}

	async def receive() -> dict[str, Any]:
		return {"type": "http.request", "body": b"", "more_body": False}

	return Request(scope, receive)  # type: ignore[arg-type]
