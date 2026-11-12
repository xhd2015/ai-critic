import SwiftUI

/// wrk projects / worktrees. iTerm actions are optional (local only).
@available(macOS 15.0, *)
public struct ProjectsPage: View {
    public let projects: [WrkProjectStatus]
    public let loading: Bool
    public let loadError: String?
    public let notConfigured: Bool
    public var onOpenInITerm: ((String) -> Void)?
    public var onNewWorktree: ((WrkProjectStatus) -> Void)?
    /// Fired when the page appears: loads the project list + per-project git state.
    /// The app does not poll projects in the background, so the page drives it.
    public var onRefresh: (() async -> Void)?

    public init(
        projects: [WrkProjectStatus],
        loading: Bool,
        loadError: String?,
        notConfigured: Bool = false,
        onOpenInITerm: ((String) -> Void)? = nil,
        onNewWorktree: ((WrkProjectStatus) -> Void)? = nil,
        onRefresh: (() async -> Void)? = nil
    ) {
        self.projects = projects
        self.loading = loading
        self.loadError = loadError
        self.notConfigured = notConfigured
        self.onOpenInITerm = onOpenInITerm
        self.onNewWorktree = onNewWorktree
        self.onRefresh = onRefresh
    }

    public var body: some View {
        Group {
            if notConfigured {
                ContentUnavailableView("Not configured", systemImage: "folder")
            } else {
                let status = ProjectsMenuFormatter.formatProjectsListStatusLabel(
                    loading: loading,
                    count: projects.count,
                    err: loadError ?? ""
                )
                if projects.isEmpty {
                    ContentUnavailableView(
                        status.isEmpty ? ProjectsMenuFormatter.formatProjectsEmptyLabel() : status,
                        systemImage: "folder"
                    )
                } else {
                    List {
                        if loading {
                            Text(ProjectsMenuFormatter.formatProjectsLoadingLabel())
                                .foregroundStyle(.secondary)
                        }
                        ForEach(projects) { project in
                            projectSection(project)
                        }
                    }
                }
            }
        }
        .navigationTitle("Projects")
        .accessibilityIdentifier("projects-page")
        .task {
            if let onRefresh {
                await onRefresh()
            }
        }
    }

    @ViewBuilder
    private func projectSection(_ project: WrkProjectStatus) -> some View {
        let parts = ProjectsMenuFormatter.formatProjectTitleParts(
            name: project.name,
            branch: project.branch ?? "",
            clean: project.clean,
            errMsg: project.error ?? ""
        )
        Section {
            if let err = project.error, !err.isEmpty {
                Text(err).foregroundStyle(.secondary)
            } else {
                Text("Branch: \(project.branch ?? "")")
                    .foregroundStyle(.secondary)
                Text(project.clean == nil
                     ? ProjectsMenuFormatter.formatCleanPendingLabel()
                     : ProjectsMenuFormatter.formatCleanLabel(clean: project.clean))
                    .foregroundStyle(.secondary)
            }
            if let onOpenInITerm {
                Button("Open in iTerm2") { onOpenInITerm(project.path) }
            }
            let worktrees = project.worktrees ?? []
            ForEach(worktrees) { wt in
                let wtParts = ProjectsMenuFormatter.formatWorktreeTitleParts(name: wt.name, clean: wt.clean)
                HStack {
                    Text(wtParts.leading)
                    Spacer()
                    if !wtParts.trailing.isEmpty {
                        Text(wtParts.trailing).foregroundStyle(.secondary)
                    }
                    if let onOpenInITerm {
                        Button("Open") { onOpenInITerm(wt.path) }
                            .controlSize(.small)
                    }
                }
            }
            if let onNewWorktree {
                Button("New Worktree…") { onNewWorktree(project) }
            }
        } header: {
            HStack {
                Text(parts.leading)
                Spacer()
                Text(parts.trailing)
            }
        }
    }
}
