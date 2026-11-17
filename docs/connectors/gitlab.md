# GitLab

Reads your GitLab groups and projects, and what happens in them.

```yaml
kind: Connection
metadata:
  identifier: gitlab
  title: GitLab
spec:
  connector:
    type: gitlab
    config:
      url: https://gitlab.example.com     # default https://gitlab.com
      token:
        env: GP_SECRET_GITLAB_TOKEN
      groups:         # and/or projects, by full path
        - acme
      excludeArchived: true
  sync:
    interval: 15m
```

- The token needs `read_api` and Reporter on what it reads; `api` and Maintainer for [webhooks](../syncing.md#fresh-between-syncs).

For example, to keep each project's pipelines:

```yaml
sources:
  - connection: gitlab
    resource: pipelines
    mode: upsert
    entity:
      identifier: .id
      title: ._project + " " + .ref  # "acme/payments/api main"
      properties:
        repository: ._project
        status: .status  # success, failed, running...
        triggeredAt: .created_at
```

## Resources

- Read in full each sync: `groups`, `members`, `projects`, `tags`, `releases`, `environments`.
- Read as they change, per project: `merge_requests`, `issues`, `pipelines`, `jobs`, `job_attempts`, `deployments`.
- `runners`: those jobs ran on.

## Fields Worth Knowing

- Everything in a project: `_project`, its full path. `members`: `_group`.
- `projects`:
  - `_defaultBranch` (with `protected`), `_latestPipeline` (such as `._latestPipeline.status`), `_ciConfig` (CI lint: `valid`, `errors`, `jobs`...), `_files` (paths at the root and in `.gitlab/`).
  - `_mergeChecks`: `pipelineMustSucceed`, `discussionsMustBeResolved`.
  - `_vulnerabilities`: counts by severity, `byScanner`, `oldestCriticalAt`, `oldestHighAt`.
  - `_dependencies`: `count`, `osPackages`, `malware`, `licenses`.
- `environments`: `_lastDeployment`.
- `jobs` (latest attempt per job) and `job_attempts` (every attempt): `_attempt`, `_attempts`, `_failedAttempts`, `_retried`, `_flaky`, `_queuedAt`, `_waitSeconds`, `_stageWaitSeconds`, `_manual`, `_playWaitSeconds`, `_playedBy`.
- `deployments`: `_approvalRequired`, `_approvals`, `_approvedAt`, `_approvalSeconds`, `_waitSeconds`.
- `runners`: `_title`, `_groups`, `_projects`, `_url`, `_lastJobAt`.
