import Foundation

/// Serializes registry writes per branch and remembers the branch HEAD across
/// consecutive commits.
///
/// Why this exists: GitHub's `GET /git/ref/heads/<branch>` is eventually
/// consistent. Immediately after a commit, a re-read can return the *previous*
/// HEAD; committing against it makes the final `PATCH refs` (force:false) fail
/// with 409/422 — surfacing as "the registry kept changing under us" even
/// though the user is the only writer. Since this app is the sole writer to
/// the user's private registry, the HEAD we just wrote *is* the truth, so we
/// cache it and skip the stale ref read entirely. The FIFO lock also stops two
/// quick UI actions (delete, delete) from racing each other on the same branch.
public actor BranchGate {
    public static let shared = BranchGate()

    public struct Head: Sendable {
        public let commit: String
        public let tree: String

        public init(commit: String, tree: String) {
            self.commit = commit
            self.tree = tree
        }
    }

    private var heads: [String: Head] = [:]
    private var tails: [String: Task<Void, Never>] = [:]

    public init() {}

    public static func key(_ repo: RepoRef, _ branch: String) -> String {
        "\(repo.fullName)#\(branch)"
    }

    public func head(_ key: String) -> Head? { heads[key] }

    /// Record the HEAD produced by a successful commit so the next write on
    /// this branch skips the (eventually-consistent) ref read.
    public func setHead(_ key: String, commit: String, tree: String) {
        heads[key] = Head(commit: commit, tree: tree)
    }

    /// Drop the cached HEAD (e.g. after a genuine conflict — someone else
    /// really did move the branch) so the next attempt reads fresh.
    public func clearHead(_ key: String) {
        heads[key] = nil
    }

    /// Run `op` after every previously-enqueued op for `key` has finished
    /// (FIFO mutual exclusion). A throwing op does not wedge the chain.
    public func withLock<T: Sendable>(
        _ key: String, _ op: @escaping @Sendable () async throws -> T
    ) async throws -> T {
        // No suspension between reading and replacing the tail — atomic on the actor.
        let prev = tails[key]
        let task = Task { () throws -> T in
            await prev?.value
            return try await op()
        }
        tails[key] = Task { _ = try? await task.value }
        return try await task.value
    }
}
