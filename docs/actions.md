# Actions

To let people do routine work on one entity themselves, such as restarting a service, you add an action. People run it from the entity's page or from Self-Service: they fill in its form, and its steps run in order.

**Actions › New Action**:

```yaml
kind: Action
metadata:
  identifier: restart
  title: Restart
spec:
  blueprint: service  # whose entities it runs on
  run:  # who can run it, besides admins
    roles:
      - member
  inputs:
    reason:
      type: string
      required: true
  steps:
    - http:
        method: POST
        url: https://ops.example.com/services/{{ $entity }}/restart
        headers:
          Authorization:
            env: GP_SECRET_OPS_TOKEN
        body:
          reason: $input.reason
          by: $me
```

- `scope` limits it to the entities that match; `run.filter` to those a person may run it on, such as their own team's.
- Inputs are written as properties are; `relation` inputs let people pick entities.
- `$entity`, `$input.<id>`, `$me` and `$step.<id>.<field>` fill in values; `{{ }}` puts them inside text.

## Steps

- `http`: calls a URL; `output` picks fields from the answer with jq.
- `approval`: waits for approvers; one rejection ends the run.
- `entity`: writes or deletes an entity through a connection that doesn't pull.
- `check`: counts entities and fails with a message unless the count is as expected.
- `sync`: syncs a connection so earlier changes show.
- `output`: the summary and links shown when the run is done.
- Any step: `id` to refer to it later, `timeout` (default 30s), `retry`, `onFailure: continue`.

**Runs** in the sidebar shows every run with its steps.
