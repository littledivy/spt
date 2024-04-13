[![Go Reference](https://pkg.go.dev/badge/github.com/littledivy/spt.svg)](https://pkg.go.dev/github.com/littledivy/spt)

```
spt(1)

Usage:
  spt provision
  spt run
  spt self
  spt attach
  spt validate

Options:
  -h, --help  Show this screen.
  -c, --config  Configuration file [default: spt.toml]
  -d, --detach  Detach local client
  --delete  Deprovision device
  --id  Device ID
```

![spt](demo.gif)

See [`example/`](example) for example usage and configuration.

### Example configuration

```toml
# spt.toml

[project]
name = "example"

[service.equinix]
project = "EQUINIX_PROJECT"
api_key = "EQUINIX_API_KEY"
spot_price_max = 0.2
plan = "m3.small.x86"
os = "ubuntu_22_04"

[build.args]
passthrough = ["BUILD_ARG_1"]

[run.env]
passthrough = ["RUN_ENV_1"]
```
