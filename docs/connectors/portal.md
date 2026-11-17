# Portal

Reads the portal's own `users`, `groups`, `blueprints` and `connections`, such as to list the people who sign in, or the blueprints and connections you've built. It takes no config.

```yaml
kind: Connection
metadata:
  identifier: portal
  title: Portal
spec:
  connector:
    type: portal
```
