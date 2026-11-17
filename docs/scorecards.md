# Scorecards

A scorecard rates one blueprint's entities on a ladder of tiers. Readiness might ask bronze repositories to have an owner, silver ones to pass CI, and gold ones to be active lately. Others might cover security (no critical vulnerabilities, scanning on) or operations (an on-call team, a runbook).

## Writing One

```yaml
kind: Scorecard
metadata:
  identifier: readiness
  title: Readiness
spec:
  blueprint: repository
  filter:
    not:
      property: topics
      op: contains
      value: experimental
  tiers:
    - name: bronze
      title: Bronze
      color: orange
      criteria:
        hasOwner:
          title: Has an Owner
          check:
            property: owner
            op: exists
          fix: Move the project into its team's subgroup.
    - name: silver
      criteria:
        ciPasses:
          when:
            property: ci
            op: exists
          check:
            property: ci
            op: eq
            value: success
```

- **Blueprints › (blueprint) › Scorecards › New Scorecard**.
- `spec.filter` scores only the entities that match. It can't use `$me`, as there's no viewer.
- Tiers go lowest first. An entity reaches a tier by passing its checks and every lower tier's.
- Check identifiers are unique across all tiers.
- `check` is a filter: entities it matches pass. [Dashboards](dashboards.md#filters) lists the operators.
- `when` limits a check to the entities it matches; the rest skip it.
- `fix` shows beside a failing check on the entity's page.
- Logic a filter can't express goes in a computed property on the blueprint, which a check then reads.

## Example Checks

```yaml
criteria:
  hasOwner:
    title: Has an Owner
    check:
      property: owner
      op: exists
  ciPasses:
    title: CI Passes
    check:
      property: ci
      op: eq
      value: success
  active:
    title: Active in the Last 90 Days
    check:
      property: lastActivityAt
      op: within
      value: 90d
  covered:
    title: Coverage of 80% or More
    check:
      property: coverage
      op: gte
      value: 80
  noCriticals:
    title: No Critical Vulnerabilities
    check:
      property: criticalVulnerabilities
      op: eq
      value: 0
  teamOnCall:
    title: Team Has On-Call
    check:
      property: owner.onCall
      op: exists
  protected:
    title: Default Branch Protected
    when:  # only production repositories
      property: tier
      op: eq
      value: production
    check:
      property: defaultBranchProtected
      op: eq
      value: true
```

## When It Runs

- Shortly after publishing the scorecard or its blueprint.
- Periodically, hourly by default.
- On demand: **(scorecard) › Evaluate Now**.

## Reading Results

- `_scorecards.<scorecard>` on the blueprint reads an entity's tier.
- After a dot: `score` (percent of applicable checks passed), `passed`, `failed`, `skipped`, `failing`, or `checks.<id>` (`passed`, `failed` or `skipped`).
- A property with `type: scorecard` names the result and adds it as a catalog column:

  ```yaml
  properties:
    readiness:
      type: scorecard
      scorecard: readiness
  ```

- Then `readiness` is the tier and `readiness.checks.hasOwner` a check, in filters, columns, other scorecards and widgets.
- A `scorecard` widget shows how entities spread across the tiers. See [Dashboards](dashboards.md).
