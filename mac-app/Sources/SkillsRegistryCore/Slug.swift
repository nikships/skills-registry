import Foundation

/// Normalize a skill name into a filesystem-safe registry slug.
///
/// Identical algorithm to `scan.Slugify` (Go) and `slugify` (Python):
/// lowercase, trim, replace every run of non-`[a-z0-9]` chars with `_`,
/// strip leading/trailing `_`, falling back to "skill" when empty.
public func slugify(_ name: String) -> String {
    let lowered = name.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    var out = ""
    out.reserveCapacity(lowered.count)
    var pendingUnderscore = false
    for ch in lowered.unicodeScalars {
        if (ch >= "a" && ch <= "z") || (ch >= "0" && ch <= "9") {
            if pendingUnderscore && !out.isEmpty {
                out.append("_")
            }
            pendingUnderscore = false
            out.unicodeScalars.append(ch)
        } else {
            pendingUnderscore = true
        }
    }
    // Trailing run collapses to nothing (matches Python's `.strip("_")`).
    if out.isEmpty { return "skill" }
    return out
}
