import Foundation

/// Left/right split for a Projects submenu title (mirrors Go `ProjectTitleParts`).
public struct ProjectTitleParts: Equatable, Sendable {
    public let leading: String
    public let trailing: String

    public init(leading: String, trailing: String) {
        self.leading = leading
        self.trailing = trailing
    }
}

/// Left/right split for a linked worktree row (mirrors Go `WorktreeTitleParts`).
public struct WorktreeTitleParts: Equatable, Sendable {
    public let leading: String
    public let trailing: String

    public init(leading: String, trailing: String) {
        self.leading = leading
        self.trailing = trailing
    }
}

/// Projects submenu labels — mirrors `macosapp/menubar` project formatters.
public enum ProjectsMenuFormatter {
    /// `clean == nil` means the per-project git status has not been fetched yet
    /// (the list endpoint is registry-only); the row then shows "· branch" or no
    /// decoration instead of a misleading "○" dirty dot.
    public static func formatProjectTitleParts(name: String, branch: String, clean: Bool?, errMsg: String) -> ProjectTitleParts {
        if !errMsg.isEmpty {
            return ProjectTitleParts(leading: name, trailing: "⚠ Error")
        }
        guard let clean else {
            let trailing = branch.isEmpty ? "" : "· \(branch)"
            return ProjectTitleParts(leading: name, trailing: trailing)
        }
        if clean {
            return ProjectTitleParts(leading: name, trailing: "● \(branch)")
        }
        return ProjectTitleParts(leading: name, trailing: "○ \(branch)")
    }

    public static func formatWorktreeTitleParts(name: String, clean: Bool?) -> WorktreeTitleParts {
        WorktreeTitleParts(leading: name, trailing: formatCleanLabel(clean: clean))
    }

    /// Clean/dirty decoration shared by project and worktree rows.
    /// Nil clean ("not fetched yet") yields no decoration.
    public static func formatCleanLabel(clean: Bool?) -> String {
        guard let clean else { return "" }
        return clean ? "● Clean" : "○ Dirty"
    }

    /// Projects page placeholder while the on-demand git status is in flight.
    public static func formatCleanPendingLabel() -> String {
        "Status: loading…"
    }

    /// Legacy single-string title: Leading + "  " + Trailing (double space).
    public static func formatProjectTitle(name: String, branch: String, clean: Bool?, errMsg: String) -> String {
        let parts = formatProjectTitleParts(name: name, branch: branch, clean: clean, errMsg: errMsg)
        return joinTitleParts(leading: parts.leading, trailing: parts.trailing)
    }

    /// Legacy single-string worktree title: Leading + "  " + Trailing (double space).
    public static func formatWorktreeTitle(name: String, clean: Bool?) -> String {
        let parts = formatWorktreeTitleParts(name: name, clean: clean)
        return joinTitleParts(leading: parts.leading, trailing: parts.trailing)
    }

    /// Composes a row title; an empty decoration leaves no trailing spaces behind.
    public static func joinTitleParts(leading: String, trailing: String) -> String {
        trailing.isEmpty ? leading : "\(leading)  \(trailing)"
    }

    public static func formatProjectsEmptyLabel() -> String {
        "No wrk projects"
    }

    /// Unicode ellipsis U+2026 — not three ASCII periods.
    public static func formatProjectsLoadingLabel() -> String {
        "Loading…"
    }

    public static func formatProjectsLoadFailedLabel() -> String {
        "Failed to load projects"
    }

    /// Picks the disabled placeholder when the Projects menu has no project rows.
    /// Returns empty string when `count > 0` (show project menus).
    public static func formatProjectsListStatusLabel(loading: Bool, count: Int, err: String) -> String {
        if count > 0 {
            return ""
        }
        if loading {
            return formatProjectsLoadingLabel()
        }
        if !err.isEmpty {
            return formatProjectsLoadFailedLabel()
        }
        return formatProjectsEmptyLabel()
    }
}
