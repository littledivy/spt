package main

import (
  "os"
  "fmt"
  "flag"

	"github.com/BurntSushi/toml"
  "github.com/joho/godotenv"

  spt "github.com/littledivy/spt"
)

const help = `spt(3)

Usage:
  spt provision
  spt run

Options:
  -h, --help  Show this screen.
  -c, --config  Configuration file [default: spt.toml]
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

  configFile := flag.String("config", "spt.toml", "Configuration file")

  switch os.Args[1] {
  case "provision":
    provisionCmd.Parse(os.Args[2:])
  case "run":
    runCmd.Parse(os.Args[2:])
  default:
    fmt.Println("Unrecognized command:", os.Args[1])
    flag.Usage()
    os.Exit(1)
  }

  godotenv.Load()

	config, err := readConfig(*configFile)
	if err != nil {
		fmt.Println(err)
		return
	}

  client := spt.NewClient(config)
  device, err := client.Provision()
  if err != nil {
    fmt.Println(err)
    os.Exit(1)
  }

  if runCmd.Parsed() {
    device.Run()
  }
}
