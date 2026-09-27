import Foundation
import Security

/// Minimal Keychain wrapper for the GitHub user-to-server token. Stores a
/// single generic-password item keyed by service + account.
///
/// Demo mode sets `inMemoryOnly`, which reroutes every call to a
/// process-local dictionary so no call site — present or future — can read or
/// delete the user's real Keychain item from a `--demo` process.
public enum Keychain {
    public static let service = "dev.skills-registry.app"
    public static let tokenAccount = "github-user-token"

    /// When true, `get`/`set`/`delete` use `memory` instead of `SecItem*`.
    /// Set once at launch by demo mode; never toggled back in-process.
    public static var inMemoryOnly = false
    private static var memory: [String: String] = [:]

    public static func set(_ value: String, account: String = tokenAccount) {
        if inMemoryOnly { memory[account] = value; return }
        let data = Data(value.utf8)
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
        // Delete any existing item, then add fresh — simplest correct upsert.
        SecItemDelete(query as CFDictionary)
        var add = query
        add[kSecValueData as String] = data
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        SecItemAdd(add as CFDictionary, nil)
    }

    public static func get(account: String = tokenAccount) -> String? {
        if inMemoryOnly { return memory[account] }
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)
        guard status == errSecSuccess, let data = item as? Data,
              let str = String(data: data, encoding: .utf8) else {
            return nil
        }
        return str
    }

    public static func delete(account: String = tokenAccount) {
        if inMemoryOnly { memory.removeValue(forKey: account); return }
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
        SecItemDelete(query as CFDictionary)
    }
}
