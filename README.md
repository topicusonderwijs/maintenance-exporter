# About

"Maintenance Exporter" is a prometheus exporter that exports a metric to signal 
wether or not a maintenance window is active. This metric can then be used in a
[alerting rule](https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/#alerting-rules) 
to create an alert when a Maintenance Window is open. That alert on its turn can 
be used in an alertmanager 
[inhibit rule](https://prometheus.io/docs/alerting/latest/configuration/#inhibit_rule)
to granulay specify which alerts should be supressed during a maintenance window.


# Rationale
One can already define `mute_time_intervals` and `active_time_intervals` on a 
alertmanager `route`. But this then mutes the entire `route`. This is a proper 
solution for things like an "workhours" or an "on-call" route. But it is less 
suited in situations where you only want to suppress a subselection of alerts.


# Example Scenario
A company restores each night the staging environment for **component 
foo** . This creates all kinds of alerts with the labels 
`{component="foo",environment="staging"}` which they would obviously like to 
suppress during the maintenance window.


This can be achieved by defining a maintenance window in the 
`maintenance-exporter`, creating a alert when the maintenance window becomes 
active and use that alert to suppress other alerts with the help of 
AlertManagers `inhibit_rule`.

## Configuration

**BREAKING CHANGE v0.2.0**: In the cron format day `7` is not a valid expression
anymore for sunday. Use `0` instead.

### Maintenance Exporter

The following configuration describes 2 maintenance windows.
```yaml
config:
  addr: ":9099"               # default
  timezone: Europe/Amsterdam  # default: UTC
  logformat: text             # or "json"
windows:
  - name: restore staging     # Name of the maintenance window.
    cron: "0 0 * * *"         # cron expression when maintenance window should 
                              # start.
    duration: 2h              # duration of the maintenance window.
    labels:                   # Labels to add to the metric.
      component: foo 

  - name: restore staging     # Note: names can be identical, and labels can be
                              #       used to make the "maintenance window" 
                              #       unique.
    cron: "0 12 * * 0"
    timezone: UTC             # This maintenance window follows UTC instead of
                              # the global "Europe/Amsterdam" configuration.
    duration: 2h
    labels:
      component: bar
```

The metrics would then look like this:
```console
> curl http://localhost:9099/metrics
maintenance_active{component="bar",name="restore staging",configured_timezone="UTC"} 0
maintenance_active{component="foo",name="restore staging",configured_timezone="Europe/Amsterdam"} 0
```


## Prometheus - Alert Rule

Next we configure a Maintenance window alert.
```
    groups:
      - name: MaintenanceWindows
        rules:
          - alert: MaintenanceWindowOpen
            expr: maintenance_active == 1 
            for: 10s
            labels:
              severity: informational
            annotations:
              description: 'Maintenance Window OPEN: {{ $labels.name }} - {{ $labels.component }}'
              summary: 'Maintenance window is open'
```

## Alertmanager - Inhibit Rules
And finally we create a inhibit rule that makes sure that only alerts from 
**component foo** are being suppressed.

```
inhibit_rules:
    # Find an alert that satisfies this:
  - source_matchers:
      - '{alertname="MaintenanceWindowOpen", name="restore staging", component="foo"}'
    # If that alert is found, then mute alerts that satisfy this:
    target_matchers:
      - '{component="foo"}'
```

## Releases

- Do __not__ edit the Helm chart version / appVersion manually
- Changes to the draft release are overwritten by new commits, so customize the release when you are actually publishing the release
- The release tag will be used as the version for the application and Helm chart
- After publishing the release the `release` workflow will attach the binaries to the release, and publish the images and Helm chart to ghcr.io
