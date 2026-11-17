# Authentik

Reads people and groups from authentik, such as to build teams.

```yaml
kind: Connection
metadata:
  identifier: authentik
  title: Authentik
spec:
  connector:
    type: authentik
    config:
      url: https://auth.example.com
      token:  # can view users and groups
        env: GP_SECRET_AUTHENTIK_TOKEN
  sync:
    interval: 1h
```

For example, teams from the groups marked as teams, with their members:

```yaml
sources:
  - connection: authentik
    resource: groups
    filter: .attributes.team == true
    entity:
      identifier: .name | ascii_downcase    # "Payments" becomes payments
      title: .name
      properties:
        members: ._members  # ["asha", "ben"]
```

## Resources

- `users` and `groups`, read in full each sync.

## Fields Worth Knowing

- `users`: `_groups` (including through child groups), `_directGroups`.
- `groups`: `_members` (usernames, including child groups'), `_directMembers`, `_ancestors`.
- `attributes` on both hold what your admins set, such as `team: true` on a team's group.
