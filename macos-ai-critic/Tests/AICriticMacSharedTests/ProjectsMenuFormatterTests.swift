import XCTest
@testable import AICriticMacShared

/// Mirrors `macosapp/menubar/projects_test.go`: both sides must agree on row text.
final class ProjectsMenuFormatterTests: XCTestCase {
    func testProjectTitleParts() {
        XCTAssertEqual(
            ProjectsMenuFormatter.formatProjectTitleParts(name: "proj", branch: "main", clean: false, errMsg: "boom"),
            ProjectTitleParts(leading: "proj", trailing: "⚠ Error")
        )
        XCTAssertEqual(
            ProjectsMenuFormatter.formatProjectTitleParts(name: "proj", branch: "main", clean: nil, errMsg: ""),
            ProjectTitleParts(leading: "proj", trailing: "· main")
        )
        XCTAssertEqual(
            ProjectsMenuFormatter.formatProjectTitleParts(name: "proj", branch: "", clean: nil, errMsg: ""),
            ProjectTitleParts(leading: "proj", trailing: "")
        )
        XCTAssertEqual(
            ProjectsMenuFormatter.formatProjectTitleParts(name: "proj", branch: "master", clean: true, errMsg: ""),
            ProjectTitleParts(leading: "proj", trailing: "● master")
        )
        XCTAssertEqual(
            ProjectsMenuFormatter.formatProjectTitleParts(name: "proj", branch: "feature", clean: false, errMsg: ""),
            ProjectTitleParts(leading: "proj", trailing: "○ feature")
        )
    }

    func testProjectTitleHasNoTrailingSpacesWithoutDecoration() {
        let title = ProjectsMenuFormatter.formatProjectTitle(name: "proj", branch: "", clean: nil, errMsg: "")
        XCTAssertEqual(title, "proj")
        XCTAssertFalse(title.hasSuffix(" "))
    }

    func testProjectTitleJoinsWithDoubleSpace() {
        XCTAssertEqual(
            ProjectsMenuFormatter.formatProjectTitle(name: "proj", branch: "main", clean: true, errMsg: ""),
            "proj  ● main"
        )
        XCTAssertEqual(
            ProjectsMenuFormatter.formatProjectTitle(name: "proj", branch: "main", clean: nil, errMsg: "boom"),
            "proj  ⚠ Error"
        )
    }

    func testWorktreeTitleParts() {
        XCTAssertEqual(
            ProjectsMenuFormatter.formatWorktreeTitleParts(name: "wt", clean: nil),
            WorktreeTitleParts(leading: "wt", trailing: "")
        )
        XCTAssertEqual(
            ProjectsMenuFormatter.formatWorktreeTitleParts(name: "wt", clean: true),
            WorktreeTitleParts(leading: "wt", trailing: "● Clean")
        )
        XCTAssertEqual(
            ProjectsMenuFormatter.formatWorktreeTitleParts(name: "wt", clean: false),
            WorktreeTitleParts(leading: "wt", trailing: "○ Dirty")
        )
        XCTAssertEqual(ProjectsMenuFormatter.formatWorktreeTitle(name: "wt", clean: nil), "wt")
        XCTAssertEqual(ProjectsMenuFormatter.formatWorktreeTitle(name: "wt", clean: false), "wt  ○ Dirty")
    }

    func testCleanLabel() {
        XCTAssertEqual(ProjectsMenuFormatter.formatCleanLabel(clean: nil), "")
        XCTAssertEqual(ProjectsMenuFormatter.formatCleanLabel(clean: true), "● Clean")
        XCTAssertEqual(ProjectsMenuFormatter.formatCleanLabel(clean: false), "○ Dirty")
    }

    func testCleanPendingLabelUsesUnicodeEllipsis() {
        let label = ProjectsMenuFormatter.formatCleanPendingLabel()
        XCTAssertEqual(label, "Status: loading…")
        XCTAssertFalse(label.contains("..."))
    }

    func testProjectsListStatusLabel() {
        XCTAssertEqual(ProjectsMenuFormatter.formatProjectsListStatusLabel(loading: true, count: 3, err: ""), "")
        XCTAssertEqual(
            ProjectsMenuFormatter.formatProjectsListStatusLabel(loading: true, count: 0, err: ""),
            ProjectsMenuFormatter.formatProjectsLoadingLabel()
        )
        XCTAssertEqual(
            ProjectsMenuFormatter.formatProjectsListStatusLabel(loading: false, count: 0, err: "boom"),
            ProjectsMenuFormatter.formatProjectsLoadFailedLabel()
        )
        XCTAssertEqual(
            ProjectsMenuFormatter.formatProjectsListStatusLabel(loading: false, count: 0, err: ""),
            ProjectsMenuFormatter.formatProjectsEmptyLabel()
        )
    }
}
