"""Tests for the per-user link store."""

from __future__ import annotations

import pytest
from key_value.aio.stores.memory import MemoryStore

from skills_mcp.linking import LinkedRepo, LinkStore


@pytest.fixture
def store() -> LinkStore:
	return LinkStore(MemoryStore())


async def test_set_then_get_link(store: LinkStore) -> None:
	link = LinkedRepo(installation_id=42, repo="alice/skills", default_branch="main")
	await store.set_link("u-1", link)
	got = await store.get_link("u-1")
	assert got == link


async def test_get_returns_none_when_missing(store: LinkStore) -> None:
	assert await store.get_link("nobody") is None


async def test_reverse_index_populated_on_set(store: LinkStore) -> None:
	await store.set_link("u-1", LinkedRepo(7, "a/b", "main"))
	assert await store.user_for_installation(7) == "u-1"


async def test_delete_link_clears_both_halves(store: LinkStore) -> None:
	await store.set_link("u-1", LinkedRepo(7, "a/b", "main"))
	await store.delete_link("u-1")
	assert await store.get_link("u-1") is None
	assert await store.user_for_installation(7) is None


async def test_delete_installation_clears_user_too(store: LinkStore) -> None:
	await store.set_link("u-1", LinkedRepo(7, "a/b", "main"))
	await store.delete_installation(7)
	assert await store.get_link("u-1") is None
	assert await store.user_for_installation(7) is None


async def test_delete_installation_is_idempotent(store: LinkStore) -> None:
	# Deleting an install with no associated user must not raise.
	await store.delete_installation(99999)


async def test_set_link_overwrites_previous_install(store: LinkStore) -> None:
	await store.set_link("u-1", LinkedRepo(7, "a/b", "main"))
	await store.set_link("u-1", LinkedRepo(8, "a/c", "main"))
	# Forward link points at the new install.
	got = await store.get_link("u-1")
	assert got is not None
	assert got.installation_id == 8
	# New reverse mapping is in place.
	assert await store.user_for_installation(8) == "u-1"


def test_linked_repo_dict_roundtrip() -> None:
	link = LinkedRepo(installation_id=12345, repo="x/y", default_branch="trunk")
	restored = LinkedRepo.from_dict(link.to_dict())
	assert restored == link
