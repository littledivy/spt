package main

import (
  "os"
  "fmt"
  "flag"

	"github.com/BurntSushi/toml"
  "github.com/joho/godotenv"

  spt "github.com/littledivy/spt"
)

const help = `spt(1)

Usage:
  spt provision
  spt run [--detach]
  spt self [--delete]
  spt validate
  spt attach --id

Options:
  -h, --help  Show this screen.
  -c, --config  Configuration file [default: spt.toml]
  -i, --id  Device ID
  -d, --detach  Detach local client
  --delete  Deprovision device
`

func readConfig(name string) (spt.Config, error) {
	var config spt.Config
	md, err := toml.DecodeFile(name, &config)
	if err != nil {
		return config, err
	}
	spt.Log("Using configuration file: spt.toml")

	undecoded := md.Undecoded()
	if len(undecoded) > 0 {
		fmt.Printf("Following keys were not recognized:\n")
		for _, u := range undecoded {
			fmt.Printf("  %s\n", u.String())
		}
	}

	if config.Service.Equinix.Project != "" {
		spt.Log("Service: equinix")

    config.Service.Equinix.Project = os.Getenv(config.Service.Equinix.Project)
    config.Service.Equinix.ApiKey = os.Getenv(config.Service.Equinix.ApiKey)
	}

	return config, nil
}

func main() {
  flag.Usage = func() {
    fmt.Println(help)
  }

  if len(os.Args) < 2 {
    flag.Usage()
    os.Exit(1)
  }

  if os.Args[1] == "-h" || os.Args[1] == "--help" {
    flag.Usage()
    os.Exit(0)
  }

  provisionCmd := flag.NewFlagSet("provision", flag.ExitOnError)
  runCmd := flag.NewFlagSet("run", flag.ExitOnError)
  selfCmd := flag.NewFlagSet("self", flag.ExitOnError)
  validateCmd := flag.NewFlagSet("validate", flag.ExitOnError)
  attachCmd := flag.NewFlagSet("attach", flag.ExitOnError)

  detach := runCmd.Bool("d", false, "Detach local client")
  delete := selfCmd.Bool("delete", false, "Deprovision device")
  attachId := attachCmd.String("id", "", "Device ID")

  configFile := flag.String("config", "spt.toml", "Configuration file")

  switch os.Args[1] {
  case "provision":
    provisionCmd.Parse(os.Args[2:])
  case "run":
    runCmd.Parse(os.Args[2:])
  case "self":
    selfCmd.Parse(os.Args[2:])
  case "validate":
    validateCmd.Parse(os.Args[2:])
  case "attach":
    attachCmd.Parse(os.Args[2:])
  default:
    fmt.Println("Unrecognized command:", os.Args[1])
    flag.Usage()
    os.Exit(1)
  }
  rest := runCmd.Args()
  if len(rest) == 0 {
    rest = attachCmd.Args()
  }

  godotenv.Load()

  var device *spt.MetalDevice
  if selfCmd.Parsed() {
    device = spt.NewSelfDevice()
    if *delete {
      device.Delete()
    }

    return
  }

	config, err := readConfig(*configFile)
	if err != nil {
		fmt.Println(err)
		return
	}

  if validateCmd.Parsed() {
    spt.Log("OK")
    return
  }

  client := spt.NewClient(config)
  if attachCmd.Parsed() {
    device, err = client.Attach(*attachId)
  } else {
    device, err = client.Provision()
  }
  if err != nil {
    fmt.Println(err)
    os.Exit(1)
  }

  if runCmd.Parsed() || attachCmd.Parsed() {
    device.Run(*detach, rest)
  }
}
