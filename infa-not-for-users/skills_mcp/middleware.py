"""Production middleware stack for the hosted MCP server.

Three middlewares are registered in this order (see ``remote_server.py``):

1. :class:`fastmcp.server.middleware.error_handling.ErrorHandlingMiddleware`
   — outermost; converts uncaught exceptions into MCP error responses and
   suppresses internal detail leakage to clients. Pairs with the
   ``mask_error_details=True`` flag on :class:`fastmcp.FastMCP` so even tool
   handlers that raise raw ``GitHubAppError`` don't bleed status codes or
   internal paths into the LLM-visible payload.
2. :class:`fastmcp.server.middleware.rate_limiting.RateLimitingMiddleware`
   — per-user token bucket keyed on the GitHub OAuth ``sub`` claim. Bursts
   are absorbed; sustained abuse from a single account is rejected with a
   ``RateLimitError`` before reaching the tool handler.
3. :class:`fastmcp.server.middleware.logging.StructuredLoggingMiddleware`
   — innermost; logs accepted requests as JSON so we can grep for abuse
   patterns, tune limits, and audit per-user behavior.

Rate-limit thresholds are intentionally hardcoded:

* The two read tools both fan out to GitHub, so a low MCP-request rate
  already maps to a much higher GitHub-call rate (``search_skills`` on a
  large registry walks every folder).
* Tuning these numbers is a deployment-time decision that warrants a
  code review, not a Railway env-var flip.
* If a user routinely needs higher limits, that's a signal to add caching
  or pagination, not raise the cap.
"""

from __future__ import annotations

import logging

from fastmcp.server.dependencies import get_access_token
from fastmcp.server.middleware import Middleware
from fastmcp.server.middleware.error_handling import ErrorHandlingMiddleware
from fastmcp.server.middleware.logging import StructuredLoggingMiddleware
from fastmcp.server.middleware.middleware import MiddlewareContext
from fastmcp.server.middleware.rate_limiting import RateLimitingMiddleware

log = logging.getLogger("skills_mcp.middleware")


# Sustained request rate per authenticated GitHub user. 5 req/s comfortably
# covers a Claude/Cursor session opening (one search_skills + several
# get_skill calls) and stays well under GitHub's per-installation REST
# allowance even after the ``search_skills`` fan-out amplifies the call count.
MAX_REQUESTS_PER_SECOND = 5.0

# Burst budget. The typical opening pattern is `search_skills` immediately
# followed by 5-10 `get_skill` calls as the agent inspects the catalog;
# 15 absorbs that without delay while still preventing scraping floods.
BURST_CAPACITY = 15

# Constant used when an authenticated request somehow reaches the rate
# limiter without a ``sub`` claim. The auth provider rejects unauthenticated
# requests before middleware runs, so this is a defensive last-resort
# bucket — never the common path.
_ANON_CLIENT_ID = "anonymous"


def client_id_from_token(context: MiddlewareContext) -> str:
	"""Extract the rate-limit bucket key from the active access token.

	We bucket by GitHub user ``sub`` (the stable numeric account ID from
	OAuth) rather than client IP or OAuth ``client_id``. Behind shared
	NATs and inside multi-user MCP clients many honest users share both,
	so an IP-based bucket would punish them collectively. ``sub`` is
	per-account, immutable, and always present on an authenticated
	request.
	"""
	token = get_access_token()
	if token is None:
		return _ANON_CLIENT_ID
	sub = token.claims.get("sub") if token.claims else None
	return str(sub) if sub else _ANON_CLIENT_ID


def build_middleware_stack() -> list[Middleware]:
	"""Return the ordered middleware list for ``FastMCP(middleware=...)``.

	Order is meaningful: the first entry runs first on the way in and
	last on the way out. Error handling outermost catches everything;
	rate limiting next so blocked requests never reach the logger;
	structured logging innermost so it records what actually executed.
	"""
	return [
		ErrorHandlingMiddleware(
			logger=logging.getLogger("skills_mcp.errors"),
			include_traceback=False,
			transform_errors=True,
		),
		RateLimitingMiddleware(
			max_requests_per_second=MAX_REQUESTS_PER_SECOND,
			burst_capacity=BURST_CAPACITY,
			get_client_id=client_id_from_token,
			global_limit=False,
		),
		StructuredLoggingMiddleware(
			logger=logging.getLogger("skills_mcp.requests"),
			include_payload_length=True,
		),
	]


__all__ = [
	"BURST_CAPACITY",
	"MAX_REQUESTS_PER_SECOND",
	"build_middleware_stack",
	"client_id_from_token",
]
