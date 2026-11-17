# SonarQube

Reads your SonarQube projects with their measures and quality gate.

```yaml
kind: Connection
metadata:
  identifier: sonarqube
  title: SonarQube
spec:
  connector:
    type: sonarqube
    config:
      url: https://sonar.example.com
      token:
        env: GP_SECRET_SONAR_TOKEN
      projects:  # keys or patterns; default all
        - acme_*
  sync:
    interval: 1h
```

For example, coverage and quality gate per project:

```yaml
sources:
  - connection: sonarqube
    resource: projects
    entity:
      identifier: .key
      title: .name
      properties:
        coverage: ._measures.coverage  # 83.4
        qualityGate: ._qualityGate.status       # OK or ERROR
```

## Resources

- `projects`, read in full each sync, with `key`, `name` and `analysisDate`.

## Fields Worth Knowing

- `_measures`: by metric key, such as `coverage`, `ncloc`, `bugs`, `vulnerabilities`, `code_smells`, `security_hotspots`, `alert_status`, and the `*_rating` and `new_*` ones.
- `_qualityGate`: status and conditions.
- `_repository`: the bound repository's `alm` and `url`. Needs the token's user to administer the project.
