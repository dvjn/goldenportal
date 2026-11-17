# Workflows

To let people do work that isn't tied to one entity, such as restarting all of a team's services after approval, you add a workflow. People run it from Self-Service; it takes the same steps as an action, and can also run actions on many entities.

**Workflows › New Workflow**:

```yaml
kind: Workflow
metadata:
  identifier: restart-services
  title: Restart Services
spec:
  group: Operate  # the Self-Service group it shows under
  run:
    roles:
      - member
  inputs:
    services:
      relation: service
      many: true
      required: true
    reason:
      type: string
      required: true
  steps:
    - approval:
        approvers:
          groups:
            - platform
        require: 1
        timeout: 24h
    - action:
        name: restart  # the action above, on each service picked
        entities: $input.services
        inputs:
          reason: $input.reason
```

- Each action it runs still checks that the person may run it.
- Every step of an [action](actions.md#steps) works here too.
- `action` runs an action on up to 100 entities; `maxFailures` sets how many may fail before the step does.
