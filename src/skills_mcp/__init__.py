"""skills-registry — GitHub-backed skill registry tooling."""

from importlib.metadata import PackageNotFoundError, version

try:
	__version__ = version("skills-registry")
except PackageNotFoundError:
	__version__ = "0.0.0+unknown"

__all__ = ["__version__"]
