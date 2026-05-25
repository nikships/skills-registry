"""Per-user registry linking state.

Maps GitHub user IDs (the ``sub`` claim from OAuth) to the installation +
registry repo the user has selected. Stored in the same py-key-value-aio
backend that FastMCP uses for its OAuth state, but in separate collections
so the two namespaces never collide.

State layout:

* Collection ``users``: ``user_id (str) → LinkedRepo`` (the active link).
* Collection ``installs``: ``installation_id (str) → user_id (str)`` (reverse
  lookup, populated by ``installation.created`` / ``installation.deleted``
  webhooks).

We keep ``installs`` as a reverse index so the webhook path can do its job
without first knowing the user — GitHub sends us ``installation_id`` and
``account.id``, and we hash them together to materialize a link record.

The store is encrypted at rest via :class:`FernetEncryptionWrapper` (set up
in :mod:`remote_server`); this module is transport-agnostic and just calls
the async KV interface.
"""

from __future__ import annotations

import logging
from dataclasses import asdict, dataclass
from typing import Any

log = logging.getLogger("skills_mcp.linking")

_USERS_COLLECTION = "users"
_INSTALLS_COLLECTION = "installs"


@dataclass(frozen=True)
class LinkedRepo:
	"""The repo a user has selected as their skills registry."""

	installation_id: int
	repo: str  # ``owner/repo``
	default_branch: str

	def to_dict(self) -> dict[str, Any]:
		return asdict(self)

	@classmethod
	def from_dict(cls, data: dict[str, Any]) -> LinkedRepo:
		return cls(
			installation_id=int(data["installation_id"]),
			repo=str(data["repo"]),
			default_branch=str(data.get("default_branch") or "main"),
		)


class LinkStore:
	"""Async wrapper around the KV backend with typed accessors.

	The underlying ``py-key-value-aio`` API stores ``Mapping[str, Any]``
	values, so we always pass dicts — no manual JSON encoding. The Fernet
	wrapper higher up in the stack is what guarantees encryption at rest.
	"""

	def __init__(self, kv: Any) -> None:
		# ``Any`` rather than the async key_value.aio Protocol — avoids a
		# hard import on the KV dep and lets tests swap in a fake.
		self._kv = kv

	# ------------------------------------------------------------ user link

	async def get_link(self, user_id: str) -> LinkedRepo | None:
		raw = await self._kv.get(collection=_USERS_COLLECTION, key=user_id)
		if raw is None:
			return None
		try:
			return LinkedRepo.from_dict(raw)
		except (ValueError, KeyError, TypeError) as exc:
			log.warning("Discarding corrupt link for user %s: %s", user_id, exc)
			return None

	async def set_link(self, user_id: str, link: LinkedRepo) -> None:
		await self._kv.put(
			collection=_USERS_COLLECTION,
			key=user_id,
			value=link.to_dict(),
		)
		# Keep the reverse index fresh on every write so webhook lookups
		# don't lag the user-initiated repo switch.
		await self._kv.put(
			collection=_INSTALLS_COLLECTION,
			key=str(link.installation_id),
			value={"user_id": user_id},
		)
		log.info("Linked user %s → %s (install %s)", user_id, link.repo, link.installation_id)

	async def delete_link(self, user_id: str) -> None:
		# Use the typed accessor so corrupt rows silently drop the reverse
		# half (we still wipe the forward entry below either way).
		link = await self.get_link(user_id)
		if link is not None:
			await self._kv.delete(
				collection=_INSTALLS_COLLECTION,
				key=str(link.installation_id),
			)
		await self._kv.delete(collection=_USERS_COLLECTION, key=user_id)
		log.info("Unlinked user %s", user_id)

	# --------------------------------------------------------- reverse index

	async def user_for_installation(self, installation_id: int) -> str | None:
		raw = await self._kv.get(
			collection=_INSTALLS_COLLECTION,
			key=str(installation_id),
		)
		if not isinstance(raw, dict):
			return None
		user_id = raw.get("user_id")
		return str(user_id) if user_id else None

	async def delete_installation(self, installation_id: int) -> None:
		"""Drop both halves of the link for ``installation_id``.

		Used by the ``installation.deleted`` and ``installation.suspend``
		webhook handlers — when the user uninstalls the App, we forget
		everything we knew about them.
		"""
		user_id = await self.user_for_installation(installation_id)
		if user_id is not None:
			await self._kv.delete(collection=_USERS_COLLECTION, key=user_id)
		await self._kv.delete(
			collection=_INSTALLS_COLLECTION,
			key=str(installation_id),
		)
		log.info("Dropped installation %s (user=%s)", installation_id, user_id)
