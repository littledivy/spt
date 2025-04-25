package spt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/equinix/equinix-sdk-go/services/metalv1"
	metal "github.com/equinix/equinix-sdk-go/services/metalv1"
)

type (
	Config struct {
		Service Service
		Project Project
		Build   Build
		Run     Run
	}

	Service struct {
		Equinix struct {
			Project         string
			ApiKey          string  `toml:"api_key"`
			SpotPriceMax    float32 `toml:"spot_price_max"`
			Plan            string
			OperatingSystem string `toml:"os"`
		}
		AWS struct {
			Region        string
			AccessKey     string `toml:"access_key"`
			SecretKey     string `toml:"secret_key"`
			SessionToken  string `toml:"session_token"`
			InstanceType  string `toml:"instance_type"`
			AMI           string
			SecurityGroup string  `toml:"security_group"`
			SpotPriceMax  float32 `toml:"spot_price_max"`
			KeyName       string  `toml:"key_name"`
		}
	}

	Project struct {
		Name string
	}

	Build struct {
		Args struct {
			Passthrough []string
		}
	}

	Run struct {
		Env struct {
			Passthrough []string
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
	SetCustomdata(map[string]interface{})
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

func fetchMetadata() (Metadata, bool) {
	url := "http://metadata.platformequinix.com/metadata"
	client := &http.Client{
		Timeout: 2 * time.Second,
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return Metadata{}, false
	}

	resp, err := client.Do(req)
	if err != nil {
		return Metadata{}, false
	}

	if resp.StatusCode != 200 {
		return Metadata{}, false
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return Metadata{}, false
	}

	var metadata Metadata
	err = json.Unmarshal(body, &metadata)
	if err != nil {
		return Metadata{}, false
	}

	return metadata, true
}

func fetchAWSMetadata() (string, bool) {
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	// Check if it's an EC2 instance by hitting the metadata service
	tokenUrl := "http://169.254.169.254/latest/api/token"
	req, err := http.NewRequest("PUT", tokenUrl, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "60")

	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", false
	}

	tokenBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", false
	}
	token := string(tokenBody)

	instanceUrl := "http://169.254.169.254/latest/meta-data/instance-id"
	req, err = http.NewRequest("GET", instanceUrl, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("X-aws-ec2-metadata-token", token)

	resp, err = client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", false
	}

	instanceBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", false
	}

	return string(instanceBody), true
}

type Metadata struct {
	Customdata struct {
		ApiKey string `json:"api_key"`
	} `json:"customdata"`
	Id string `json:"id"`
}

func NewSelfDevice() Device {
	// Try Equinix Metal first
	metadata, ok := fetchMetadata()
	if ok {
		config := metal.NewConfiguration()
		config.AddDefaultHeader("X-Auth-Token", metadata.Customdata.ApiKey)
		client := metal.NewAPIClient(config)

		device, _, err := client.DevicesApi.FindDeviceById(context.TODO(), metadata.Id).Execute()
		if err != nil {
			log.Fatal(err)
		}

		return &MetalDevice{device: device, client: client}
	}

	// Try AWS EC2
	instanceId, ok := fetchAWSMetadata()
	if ok {
		Log("Detected AWS EC2 instance: %s", instanceId)

		// Get region from instance metadata
		awsCfg, err := config.LoadDefaultConfig(context.TODO())
		if err != nil {
			log.Fatal(err)
		}

		ec2Client := ec2.NewFromConfig(awsCfg)

		// Get the instance's public IP to show in logs
		input := &ec2.DescribeInstancesInput{
			InstanceIds: []string{instanceId},
		}

		result, err := ec2Client.DescribeInstances(context.TODO(), input)
		if err != nil {
			log.Fatal(err)
		}

		var ipAddr string
		if len(result.Reservations) > 0 && len(result.Reservations[0].Instances) > 0 {
			instance := result.Reservations[0].Instances[0]
			if instance.PublicIpAddress != nil {
				ipAddr = *instance.PublicIpAddress
			}
		}

		return &AWSInstance{
			instanceId: instanceId,
			ipAddr:     ipAddr,
			client:     ec2Client,
		}
	}

	log.Fatal("Could not determine instance type. Are you running on Equinix Metal or AWS EC2?")
	return nil // unreachable
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
sudo usermod -aG docker ubuntu
systemctl restart docker
`

type Device interface {
	Run(detach bool, args []string)
	Delete()
}

type Client struct {
	metal  *metal.APIClient
	ec2    *ec2.Client
	config Config
}

func NewAWSClient(cfg Config) (*Client, error) {
	accessKey := cfg.Service.AWS.AccessKey
	secretKey := cfg.Service.AWS.SecretKey
	sessionToken := cfg.Service.AWS.SessionToken
	region := cfg.Service.AWS.Region

	awsCfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(region),
		config.WithCredentialsProvider(aws.CredentialsProviderFunc(
			func(ctx context.Context) (aws.Credentials, error) {
				return aws.Credentials{
					AccessKeyID:     accessKey,
					SecretAccessKey: secretKey,
					SessionToken:    sessionToken,
				}, nil
			},
		)),
	)
	if err != nil {
		return nil, err
	}

	ec2Client := ec2.NewFromConfig(awsCfg)
	return &Client{ec2: ec2Client, config: cfg}, nil
}

func (c *Client) Provision() (Device, error) {
	config := c.config

	// Check which provider to use
	if config.Service.AWS.Region != "" {
		return c.provisionAWS()
	}

	return c.provisionEquinix()
}

func (c *Client) provisionAWS() (*AWSInstance, error) {
	config := c.config

	Log("Provisioning AWS spot instance")

	userData := base64.StdEncoding.EncodeToString([]byte(userScript))

	spotPrice := fmt.Sprintf("%f", config.Service.AWS.SpotPriceMax)

	input := &ec2.RequestSpotInstancesInput{
		InstanceCount: aws.Int32(1),
		SpotPrice:     aws.String(spotPrice),
		LaunchSpecification: &types.RequestSpotLaunchSpecification{
			ImageId:      aws.String(config.Service.AWS.AMI),
			InstanceType: types.InstanceType(config.Service.AWS.InstanceType),
			UserData:     aws.String(userData),
			SecurityGroupIds: []string{
				config.Service.AWS.SecurityGroup,
			},
			KeyName: func() *string {
				if config.Service.AWS.KeyName != "" {
					return aws.String(config.Service.AWS.KeyName)
				}
				return nil
			}(),
		},
	}

	result, err := c.ec2.RequestSpotInstances(context.TODO(), input)
	if err != nil {
		return nil, err
	}

	if len(result.SpotInstanceRequests) == 0 {
		return nil, fmt.Errorf("no spot instance requests returned")
	}

	spotRequestId := *result.SpotInstanceRequests[0].SpotInstanceRequestId
	Log("Spot request %s created, waiting for instance", spotRequestId)

	var instanceId string
	describeInput := &ec2.DescribeSpotInstanceRequestsInput{
		SpotInstanceRequestIds: []string{spotRequestId},
	}

	for {
		describeResult, err := c.ec2.DescribeSpotInstanceRequests(context.TODO(), describeInput)
		if err != nil {
			return nil, err
		}

		if len(describeResult.SpotInstanceRequests) == 0 {
			return nil, fmt.Errorf("spot instance request not found")
		}

		req := describeResult.SpotInstanceRequests[0]
		if req.State == types.SpotInstanceStateFailed {
			return nil, fmt.Errorf("spot instance request failed: %s", *req.Status.Message)
		}

		if req.InstanceId != nil {
			instanceId = *req.InstanceId
			Log("Instance %s created, waiting for it to be ready", instanceId)
			break
		}

		time.Sleep(5 * time.Second)
	}

	instanceInput := &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceId},
	}

	var ipAddr string
	for {
		instanceResult, err := c.ec2.DescribeInstances(context.TODO(), instanceInput)
		if err != nil {
			return nil, err
		}

		if len(instanceResult.Reservations) == 0 || len(instanceResult.Reservations[0].Instances) == 0 {
			return nil, fmt.Errorf("instance not found")
		}

		instance := instanceResult.Reservations[0].Instances[0]

		if instance.State.Name == types.InstanceStateNameRunning {
			if instance.PublicIpAddress != nil {
				ipAddr = *instance.PublicIpAddress
				Log("Instance is running at IP %s", ipAddr)
				break
			}
		}

		if instance.State.Name == types.InstanceStateNameTerminated {
			return nil, fmt.Errorf("instance was terminated")
		}

		time.Sleep(5 * time.Second)
	}

	Log("Waiting for SSH to be available...")
	for i := 0; i < 30; i++ {
		cmd := exec.Command("nc", "-z", "-w", "1", ipAddr, "22")
		if err := cmd.Run(); err == nil {
			break
		}
		time.Sleep(5 * time.Second)
	}

	time.Sleep(30 * time.Second)

	awsInstance := &AWSInstance{
		instanceId: instanceId,
		ipAddr:     ipAddr,
		client:     c.ec2,
		config:     config,
	}

	return awsInstance, nil
}

func (c *Client) provisionEquinix() (*MetalDevice, error) {
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
	dc.SetHostname(config.Project.Name + "-spt-instance")
	dc.SetUserdata(userScript)
	dc.SetCustomdata(map[string]interface{}{"api_key": config.Service.Equinix.ApiKey})

	if config.Service.Equinix.SpotPriceMax != 0 {
		dc.SetSpotPriceMax(config.Service.Equinix.SpotPriceMax)
	}
	if config.Service.Equinix.Plan != "" {
		dc.SetPlan(config.Service.Equinix.Plan)
	}
	if config.Service.Equinix.OperatingSystem != "" {
		dc.SetOperatingSystem(config.Service.Equinix.OperatingSystem)
	}

	Log("Provisioning Equinix Metal spot instance")

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

	Log("IP %s", ipAddr)
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

	metalDevice := &MetalDevice{device: newDevice, ipAddr: ipAddr, client: client, config: config}
	return metalDevice, nil
}

func (c *Client) Attach(id string) (Device, error) {
	if len(id) > 2 && id[:2] == "i-" {
		return c.attachAWS(id)
	}

	return c.attachEquinix(id)
}

func (c *Client) attachAWS(instanceId string) (*AWSInstance, error) {
	input := &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceId},
	}

	result, err := c.ec2.DescribeInstances(context.TODO(), input)
	if err != nil {
		return nil, err
	}

	if len(result.Reservations) == 0 || len(result.Reservations[0].Instances) == 0 {
		return nil, fmt.Errorf("AWS instance not found: %s", instanceId)
	}

	instance := result.Reservations[0].Instances[0]

	if instance.State.Name != types.InstanceStateNameRunning {
		return nil, fmt.Errorf("AWS instance %s is not running (state: %s)", instanceId, instance.State.Name)
	}

	if instance.PublicIpAddress == nil {
		return nil, fmt.Errorf("AWS instance %s has no public IP address", instanceId)
	}

	ipAddr := *instance.PublicIpAddress

	Log("Attached to AWS instance %s at IP %s", instanceId, ipAddr)

	awsInstance := &AWSInstance{
		instanceId: instanceId,
		ipAddr:     ipAddr,
		client:     c.ec2,
		config:     c.config,
	}

	return awsInstance, nil
}

func (c *Client) attachEquinix(id string) (*MetalDevice, error) {
	device, _, err := c.metal.DevicesApi.FindDeviceById(context.TODO(), id).Execute()
	if err != nil {
		return nil, err
	}

	var ipAddr string
	for _, ip := range device.GetIpAddresses() {
		if ip.GetPublic() && ip.GetAddressFamily() == 4 {
			ipAddr = ip.GetAddress()
		}
	}

	Log("Attached to Equinix Metal device %s at IP %s", id, ipAddr)
	metalDevice := &MetalDevice{device: device, ipAddr: ipAddr, client: c.metal, config: c.config}
	return metalDevice, nil
}

// Common run logic for all device types
func runRemoteDocker(ipAddr string, config Config, detach bool, args []string) {
	// Setup SSH
	sshHost := fmt.Sprintf("ssh://ubuntu@%s", ipAddr)
	Log(sshHost)

	cmd := exec.Command("ssh-keygen", "-R", ipAddr)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()

	waitForInit := "cloud-init status --wait"
	cmd = exec.Command("ssh", "-o", "StrictHostKeyChecking=no", sshHost, waitForInit)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		fmt.Println(err)
		return
	}

	cmd = exec.Command("docker", "context", "rm", "remote2")
	cmd.Run()

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
	cmd = exec.Command("docker", "--context", "remote2", "build")
	for _, arg := range config.Build.Args.Passthrough {
		cmd.Args = append(cmd.Args, "--build-arg", arg)
	}
	cmd.Args = append(cmd.Args, "--ssh", "default", "-t", name, ".")

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		fmt.Println(err)
		return
	}

	Log("Running docker image. Detached: %v", detach)
	cmd = exec.Command("docker", "--context", "remote2", "run")
	if detach {
		cmd.Args = append(cmd.Args, "-d")
	}
	for _, env := range config.Run.Env.Passthrough {
		cmd.Args = append(cmd.Args, "-e", env)
	}
	cmd.Args = append(cmd.Args, "--rm", "-t", "-i", name)
	cmd.Args = append(cmd.Args, args...)

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
}

// Equinix Metal implementation
type MetalDevice struct {
	device *metal.Device
	client *metal.APIClient
	config Config
	ipAddr string
}

func (c *MetalDevice) Run(detach bool, args []string) {
	runRemoteDocker(c.ipAddr, c.config, detach, args)

	if !detach {
		c.Delete()
	}
}

func (c *MetalDevice) Delete() {
	Log("De-provisioning the Equinix Metal spot instance")
	_, err := c.client.DevicesApi.DeleteDevice(context.TODO(), c.device.GetId()).Execute()
	if err != nil {
		fmt.Println(err)
		return
	}
}

// AWS implementation
type AWSInstance struct {
	instanceId string
	client     *ec2.Client
	config     Config
	ipAddr     string
}

func (c *AWSInstance) Run(detach bool, args []string) {
	runRemoteDocker(c.ipAddr, c.config, detach, args)

	if !detach {
		c.Delete()
	}
}

func (c *AWSInstance) Delete() {
	Log("Terminating the AWS spot instance")
	_, err := c.client.TerminateInstances(context.TODO(), &ec2.TerminateInstancesInput{
		InstanceIds: []string{c.instanceId},
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	// Get spot instance request ID
	describeInput := &ec2.DescribeInstancesInput{
		InstanceIds: []string{c.instanceId},
	}
	result, err := c.client.DescribeInstances(context.TODO(), describeInput)
	if err != nil {
		fmt.Println("Error getting spot instance request ID:", err)
		return
	}

	if len(result.Reservations) > 0 && len(result.Reservations[0].Instances) > 0 {
		instance := result.Reservations[0].Instances[0]
		if instance.SpotInstanceRequestId != nil {
			spotRequestId := *instance.SpotInstanceRequestId
			Log("Canceling spot request %s", spotRequestId)

			// Cancel the spot instance request
			cancelInput := &ec2.CancelSpotInstanceRequestsInput{
				SpotInstanceRequestIds: []string{spotRequestId},
			}
			_, err = c.client.CancelSpotInstanceRequests(context.TODO(), cancelInput)
			if err != nil {
				fmt.Println("Error canceling spot instance request:", err)
				return
			}
			Log("Spot request %s canceled", spotRequestId)
		}
	}
}
