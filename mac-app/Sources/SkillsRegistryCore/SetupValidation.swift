import Foundation

/// Client-side validation for the Setup screen's two inputs, mirroring
/// GitHub's repository naming rules so invalid input is caught before any
/// network call. Each function returns a short inline hint, or `nil` when the
/// input is valid (or empty — the buttons stay disabled and hint-free until
/// the user types something).
public enum SetupValidation {
    private static let nameChars =
        CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-")

    /// Validate a new repository name: non-empty, ≤100 characters, only
    /// `[A-Za-z0-9._-]`, and no leading/trailing dots.
    public static func repoNameHint(_ raw: String) -> String? {
        let name = raw.trimmingCharacters(in: .whitespaces)
        if name.isEmpty { return nil }
        if name.count > 100 { return "Names can't be longer than 100 characters." }
        if name.hasPrefix(".") || name.hasSuffix(".") {
            return "Names can't start or end with a dot."
        }
        if name.rangeOfCharacter(from: nameChars.inverted) != nil {
            return "Only letters, numbers, dots, underscores, and dashes."
        }
        return nil
    }

    /// Validate a manual `owner/repo` connect string: exactly one slash, no
    /// whitespace, and a valid repository name segment.
    public static func repoRefHint(_ raw: String) -> String? {
        let ref = raw.trimmingCharacters(in: .whitespaces)
        if ref.isEmpty { return nil }
        if ref.rangeOfCharacter(from: .whitespacesAndNewlines) != nil {
            return "Use owner/repo with no spaces."
        }
        guard RepoRef(fullName: ref) != nil else {
            return "Enter a repository as owner/repo."
        }
        return nil
    }
}
