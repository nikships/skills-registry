import XCTest
@testable import SkillsRegistryCore

/// Both tree readers must honor GitHub's recursive-tree `truncated` flag.
/// Writes (publish/delete) refuse outright — a partial listing would leave
/// stale files behind or report a false slug-not-found — while reads return
/// partial data flagged `truncated` (or `.treeTruncated` when the target is
/// absent from the partial listing). Runs against `StubGitHub` (see
/// BranchGateTests.swift), so nothing here touches the network.
final class GitHubTruncationTests: XCTestCase {
    override func tearDown() {
        StubGitHub.reset()
        super.tearDown()
    }

    private func makeAPI() -> GitHubAPI {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [StubURLProtocol.self]
        return GitHubAPI(token: "t", session: URLSession(configuration: cfg))
    }

    /// Unique repo per test — BranchGate.shared is process-global.
    private func seedRepo(files: [String: String], truncated: Bool) -> RepoRef {
        let repo = RepoRef(owner: "u", name: "reg-\(UUID().uuidString.prefix(8))")
        StubGitHub.seed(repo: repo, branch: "main", files: files)
        StubGitHub.truncatedTrees = truncated
        return repo
    }

    // MARK: - writes refuse

    func testPublishRefusesTruncatedTree() async throws {
        let repo = seedRepo(files: ["alpha/SKILL.md": "---\nname: alpha\n---\nBody."], truncated: true)
        do {
            _ = try await makeAPI().publish(repo, slug: "alpha",
                                            files: ["SKILL.md": Data("new".utf8)],
                                            message: "publish: alpha", branch: "main")
            XCTFail("publish committed against a truncated listing")
        } catch let e as WriteError {
            guard case .treeTruncated = e else { return XCTFail("got \(e)") }
        }
        XCTAssertEqual(StubGitHub.commitCount, 0, "no commit may land after a refusal")
    }

    func testDeleteRefusesTruncatedTree() async throws {
        // The slug IS present in the partial listing — entries past the cutoff
        // would still survive the null-SHA sweep, so this must refuse too.
        let repo = seedRepo(files: ["alpha/SKILL.md": "hi"], truncated: true)
        do {
            _ = try await makeAPI().delete(repo, slug: "alpha", message: "remove: alpha", branch: "main")
            XCTFail("delete committed against a truncated listing")
        } catch let e as WriteError {
            guard case .treeTruncated = e else { return XCTFail("got \(e)") }
        }
        XCTAssertEqual(StubGitHub.commitCount, 0, "no commit may land after a refusal")
    }

    func testDeleteReportsTruncationNotSlugNotFound() async throws {
        // Absent from a partial listing: truncation, not a false not-found.
        let repo = seedRepo(files: ["alpha/SKILL.md": "hi"], truncated: true)
        do {
            _ = try await makeAPI().delete(repo, slug: "missing", message: "remove: missing", branch: "main")
            XCTFail("expected a truncation error")
        } catch let e as WriteError {
            guard case .treeTruncated = e else { return XCTFail("got \(e), want .treeTruncated") }
        }
    }

    // MARK: - reads flag partial data

    func testListSkillsFlagsTruncatedListing() async throws {
        let repo = seedRepo(files: [
            "alpha/SKILL.md": "---\nname: Alpha\ndescription: First.\n---\nBody.",
            "beta/SKILL.md": "---\nname: Beta\ndescription: Second.\n---\nBody.",
        ], truncated: true)
        let result = try await makeAPI().listSkills(repo, branch: "main")
        XCTAssertTrue(result.truncated)
        XCTAssertEqual(result.value.map(\.slug), ["alpha", "beta"])
        XCTAssertEqual(result.value.first?.name, "Alpha")
    }

    func testListSkillsUnflaggedWhenComplete() async throws {
        let repo = seedRepo(files: ["alpha/SKILL.md": "---\nname: Alpha\n---\nBody."], truncated: false)
        let result = try await makeAPI().listSkills(repo, branch: "main")
        XCTAssertFalse(result.truncated)
        XCTAssertEqual(result.value.map(\.slug), ["alpha"])
    }

    func testGetSkillFlagsTruncatedDetail() async throws {
        let repo = seedRepo(files: [
            "alpha/SKILL.md": "---\nname: Alpha\n---\nBody.",
            "alpha/extra.md": "more",
        ], truncated: true)
        let detail = try await makeAPI().getSkill(repo, slug: "alpha", branch: "main")
        XCTAssertTrue(detail.truncated)
        XCTAssertEqual(detail.files, ["SKILL.md", "extra.md"])
    }

    func testGetSkillReportsTruncationNotFalseNotFound() async throws {
        let repo = seedRepo(files: ["alpha/SKILL.md": "hi"], truncated: true)
        do {
            _ = try await makeAPI().getSkill(repo, slug: "missing", branch: "main")
            XCTFail("expected a truncation error")
        } catch let e as ReadError {
            guard case .treeTruncated("missing") = e else { return XCTFail("got \(e)") }
        } catch {
            XCTFail("truncation must not surface as a 404 GitHubError: \(error)")
        }
    }

    func testSkillFileDataFlagsTruncatedFiles() async throws {
        let repo = seedRepo(files: ["alpha/SKILL.md": "hi"], truncated: true)
        let result = try await makeAPI().skillFileData(repo, slug: "alpha", branch: "main")
        XCTAssertTrue(result.truncated)
        XCTAssertEqual(result.value["SKILL.md"], Data("hi".utf8))
    }

    func testSkillFileDataReportsTruncationNotFalseNotFound() async throws {
        let repo = seedRepo(files: ["alpha/SKILL.md": "hi"], truncated: true)
        do {
            _ = try await makeAPI().skillFileData(repo, slug: "missing", branch: "main")
            XCTFail("expected a truncation error")
        } catch let e as ReadError {
            guard case .treeTruncated("missing") = e else { return XCTFail("got \(e)") }
        } catch {
            XCTFail("truncation must not surface as a 404 GitHubError: \(error)")
        }
    }
}
