import XCTest
@testable import SkillsRegistryCore

final class DetailCopyTests: XCTestCase {
    func testCopyTargetUsesFullMarkdownForSkillMd() {
        let target = SkillDetail.copyTarget(selectedFile: "SKILL.md", auxText: nil, markdown: "---\nname: x\n---\nbody")
        XCTAssertEqual(target?.name, "SKILL.md")
        XCTAssertEqual(target?.text, "---\nname: x\n---\nbody")
    }

    func testCopyTargetFollowsVisibleSupportFile() {
        let target = SkillDetail.copyTarget(selectedFile: "scripts/run.sh", auxText: "#!/usr/bin/env bash\n", markdown: "md")
        XCTAssertEqual(target?.name, "scripts/run.sh")
        XCTAssertEqual(target?.text, "#!/usr/bin/env bash\n")
    }

    func testCopyTargetIsNilWhileSupportFileLoads() {
        XCTAssertNil(SkillDetail.copyTarget(selectedFile: "scripts/run.sh", auxText: nil, markdown: "md"))
    }

    func testCopyTargetKeepsSkillMdWhenAuxTextIsLeftOver() {
        let target = SkillDetail.copyTarget(selectedFile: "SKILL.md", auxText: "stale", markdown: "full")
        XCTAssertEqual(target?.name, "SKILL.md")
        XCTAssertEqual(target?.text, "full")
    }

    func testCopyTargetCopiesAnEmptySupportFile() {
        let target = SkillDetail.copyTarget(selectedFile: "notes.txt", auxText: "", markdown: "md")
        XCTAssertEqual(target?.name, "notes.txt")
        XCTAssertEqual(target?.text, "")
    }
}
