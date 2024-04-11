package spt

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/equinix/equinix-sdk-go/services/metalv1"
	metal "github.com/equinix/equinix-sdk-go/services/metalv1"
)

// import (
//  "github.com/docker/docker/api/types"
//  dclient "github.com/docker/docker/client"
//  "github.com/docker/docker/pkg/jsonmessage"
//  "github.com/moby/term"
// )

type (
	Config struct {
		Service Service
		Project Project
	}

	Project struct {
		Name string
	}

	Service struct {
		Equinix struct {
			Project string
			ApiKey  string `toml:"api_key"`
      SpotPriceMax float32 `toml:"spot_price_max"`
      Plan string
      OperatingSystem string `toml:"os"`
		}
	}
)

func Log(format string, args ...interface{}) {
	fmt.Printf("-- "+format+"\n", args...)
}

type DeviceCreator interface {
	SetPlan(string)
	SetOperatingSystem(string)
	SetHostname(string)
	SetUserdata(string)
	SetTags([]string)
	SetHardwareReservationId(string)
	SetBillingCycle(metalv1.DeviceCreateInputBillingCycle)
	SetSpotInstance(bool)
	SetSpotPriceMax(float32)
	SetTerminationTime(time.Time)
}

type OneOfDeviceCreator interface {
	DeviceCreator
	GetActualInstance() interface{}
}

var _ DeviceCreator = (*metal.DeviceCreateInMetroInput)(nil)
var _ DeviceCreator = (*metal.DeviceCreateInFacilityInput)(nil)

func NewClient(cfg Config) Client {
  apiKey := cfg.Service.Equinix.ApiKey
	config := metal.NewConfiguration()
	config.AddDefaultHeader("X-Auth-Token", apiKey)

	client := metal.NewAPIClient(config)
  return Client{metal: client, config: cfg}
}

const userScript = `#!/bin/bash
#!/bin/bash
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get upgrade -y
# install docker
apt-get install -y ca-certificates curl gnupg lsb-release unzip
mkdir -p /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null
apt-get update
apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin make
echo '{ "userland-proxy": false }' > /etc/docker/daemon.json
systemctl restart docker
`

type Client struct {
  metal *metal.APIClient
  config Config
}

func (c *Client) Provision() (*MetalDevice, error) {
	var ipAddr string
  config := c.config
  client := c.metal

	var dc DeviceCreator
	var createRequest metal.CreateDeviceRequest

	metro := "any"

	dc = &metal.DeviceCreateInMetroInput{
		Metro: metro,
	}
	createRequest = metal.CreateDeviceRequest{DeviceCreateInMetroInput: dc.(*metal.DeviceCreateInMetroInput)}
	dc.SetSpotInstance(true)
	dc.SetSpotPriceMax(0.2)
	dc.SetPlan("m3.small.x86")
	dc.SetOperatingSystem("ubuntu_22_04")
	dc.SetHostname("spt-instance")
	dc.SetUserdata(userScript)

	Log("Provisioning a spot instance")

	println(config.Service.Equinix.Project)
	projectID := config.Service.Equinix.Project
	newDevice, _, err := client.DevicesApi.CreateDevice(context.TODO(), projectID).CreateDeviceRequest(createRequest).Execute()
	if err != nil {
		return nil, err
	}

	Log("Device %s is being provisioned", newDevice.Id)

	deviceID := *newDevice.Id
	for {
		newDevice, _, err = client.DevicesApi.FindDeviceById(context.TODO(), deviceID).Execute()
		if err != nil {
			return nil, err
		}

		for _, ip := range newDevice.GetIpAddresses() {
			if ip.GetPublic() && ip.GetAddressFamily() == 4 {
				ipAddr = ip.GetAddress()
			}
		}

		if ipAddr != "" {
			break
		}

		time.Sleep(1 * time.Second)
	}

	Log("IP %s", deviceID, ipAddr)
	Log("Waiting for Provisioning...")
	stage := float32(0)
	for {
		newDevice, _, err = client.DevicesApi.FindDeviceById(context.TODO(), deviceID).Execute()
		if err != nil {
			return nil, err
		}
		if newDevice.GetState() == metal.DEVICESTATE_PROVISIONING && stage != newDevice.GetProvisioningPercentage() {
			stage = newDevice.GetProvisioningPercentage()
			Log("Provisioning %v%% complete", newDevice.GetProvisioningPercentage())
		}
		if newDevice.GetState() == metal.DEVICESTATE_ACTIVE {
			Log("Device State: %s", newDevice.GetState())
			break
		}
		time.Sleep(10 * time.Second)
	}

  metalDevice := &MetalDevice{device: newDevice, ipAddr: ipAddr, client: client}
  return metalDevice, nil
}

type MetalDevice struct {
  device *metal.Device
  client *metal.APIClient
  ipAddr string
}

func (c *MetalDevice) Run() {
	// Setup SSH
	sshHost := fmt.Sprintf("ssh://root@%s", c.ipAddr)
	Log(sshHost)

	waitForInit := "cloud-init status --wait"
	cmd := exec.Command("ssh", sshHost, waitForInit)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

  err := cmd.Run()
	if err != nil {
		fmt.Println(err)
		return
	}

	// Setup docker context
	// cli, err := dclient.NewClientWithOpts(dclient.FromEnv, dclient.WithAPIVersionNegotiation())
	// if err != nil {
	//   panic(err)
	// }
	// defer cli.Close()

	spawnCmd := fmt.Sprintf("docker context create remote2 --docker \"host=%s\"", sshHost)
	Log("Creating docker context")
	cmd = exec.Command("sh", "-c", spawnCmd)
	err = cmd.Run()
	if err != nil {
		fmt.Println(err)
		return
	}

	Log("Building docker image")
  randomId := time.Now().Unix()
	name := "spt-image-" + fmt.Sprint(randomId)
	cmd = exec.Command("docker", "--context", "remote2", "build", "-t", name, ".")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		fmt.Println(err)
		return
	}

	// resp, err := cli.ImageBuild(ctx, nil, types.ImageBuildOptions{})
	// if err != nil {
	//   fmt.Println(err)
	//   return
	// }
	// defer resp.Body.Close()
	//
	// termFd, isTerm := term.GetFdInfo(os.Stderr)
	// jsonmessage.DisplayJSONMessagesStream(resp.Body, os.Stderr, termFd, isTerm, nil)

	Log("Running docker image")
	cmd = exec.Command("docker", "--context", "remote2", "run", "--rm", "-t", "-i", name)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()

	// Cleanup
	Log("Removing docker context")
	cmd = exec.Command("docker", "context", "rm", "remote2")
	err = cmd.Run()
	if err != nil {
		fmt.Println(err)
		return
	}

	Log("De-provisioning the spot instance")
	_, err = c.client.DevicesApi.DeleteDevice(context.TODO(), c.device.GetId()).Execute()
	if err != nil {
		fmt.Println(err)
		return
	}
}
