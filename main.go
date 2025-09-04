package main

import (
	"fmt"
	"log"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/elb"
	awsx "github.com/pulumi/pulumi-awsx/sdk/v2/go/awsx/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
	"github.com/pulumiverse/pulumi-talos/sdk/go/talos/client"
	"github.com/pulumiverse/pulumi-talos/sdk/go/talos/machine"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Get some configuration values or set default values
		cfg := config.New(ctx, "")
		vpcNetworkCidr, err := cfg.Try("vpcNetworkCidr")
		if err != nil {
			vpcNetworkCidr = "10.1.0.0/18"
		}
		// talosAmi := cfg.Require("talosAmi")
		ownerTagValue, err := config.Try(ctx, "ownerTagValue")
		if err != nil {
			ownerTagValue = "nobody@nowhere.com"
		}
		teamTagValue, err := config.Try(ctx, "teamTagValue")
		if err != nil {
			teamTagValue = "TeamOfOne"
		}
		inboundAllowedIPs, err := config.Try(ctx, "allowedips")
		if err != nil {
			inboundAllowedIPs = "0.0.0.0/0"
		}

		// Create a new VPC, subnets, and associated infrastructure
		// Details: https://www.pulumi.com/registry/packages/awsx/api-docs/ec2/vpc/
		talosVpc, err := awsx.NewVpc(ctx, "talosVpc", &awsx.VpcArgs{
			EnableDnsHostnames: pulumi.Bool(true),
			EnableDnsSupport:   pulumi.Bool(true),
			CidrBlock:          &vpcNetworkCidr,
			NatGateways: &awsx.NatGatewayConfigurationArgs{
				Strategy: awsx.NatGatewayStrategySingle,
			},
			Tags: pulumi.StringMap{
				"Name":  pulumi.String("talosVpc"),
				"Owner": pulumi.String(ownerTagValue),
				"Team":  pulumi.String(teamTagValue),
			},
		})
		if err != nil {
			log.Printf("error creating security group: %s", err.Error())
			return err
		}

		// Create a security group for the Talos cluster
		// Details: https://www.pulumi.com/registry/packages/aws/api-docs/ec2/securitygroup/
		talosSg, err := ec2.NewSecurityGroup(ctx, "talosSg", &ec2.SecurityGroupArgs{
			Name:        pulumi.String("talosSg"),
			VpcId:       talosVpc.VpcId,
			Description: pulumi.String("Security group for the Talos cluster"),
			Tags: pulumi.StringMap{
				"Name":  pulumi.String("talosSg"),
				"Owner": pulumi.String(ownerTagValue),
				"Team":  pulumi.String(teamTagValue),
			},
		})
		if err != nil {
			log.Printf("error creating security group: %s", err.Error())
			return err
		}

		// Add rules to Talos security group
		// Details: https://www.pulumi.com/registry/packages/aws/api-docs/ec2/securitygrouprule/
		// First, allow all traffic within the security group
		_, err = ec2.NewSecurityGroupRule(ctx, "allowAllTalosSg", &ec2.SecurityGroupRuleArgs{
			Type:                  pulumi.String("ingress"),
			FromPort:              pulumi.Int(0),
			ToPort:                pulumi.Int(65535),
			Protocol:              pulumi.String("all"),
			SourceSecurityGroupId: talosSg.ID(),
			SecurityGroupId:       talosSg.ID(),
		})
		if err != nil {
			log.Printf("error adding rule to security group: %s", err.Error())
			return err
		}

		// Next, allow inbound access to Kubernetes APIs
		_, err = ec2.NewSecurityGroupRule(ctx, "allowK8sApi", &ec2.SecurityGroupRuleArgs{
			Type:            pulumi.String("ingress"),
			FromPort:        pulumi.Int(6443),
			ToPort:          pulumi.Int(6443),
			Protocol:        pulumi.String("tcp"),
			CidrBlocks:      pulumi.StringArray{pulumi.String(inboundAllowedIPs)},
			SecurityGroupId: talosSg.ID(),
		})
		if err != nil {
			log.Printf("error adding rule to security group: %s", err.Error())
			return err
		}

		// Allow inbound access to Talos APIs
		_, err = ec2.NewSecurityGroupRule(ctx, "allowTalosApi", &ec2.SecurityGroupRuleArgs{
			Type:            pulumi.String("ingress"),
			FromPort:        pulumi.Int(50000),
			ToPort:          pulumi.Int(50000),
			Protocol:        pulumi.String("tcp"),
			CidrBlocks:      pulumi.StringArray{pulumi.String(inboundAllowedIPs)},
			SecurityGroupId: talosSg.ID(),
		})
		if err != nil {
			log.Printf("error adding rule to security group: %s", err.Error())
			return err
		}

		// Allow all outbound traffic
		_, err = ec2.NewSecurityGroupRule(ctx, "allowEgress", &ec2.SecurityGroupRuleArgs{
			Type:            pulumi.String("egress"),
			FromPort:        pulumi.Int(0),
			ToPort:          pulumi.Int(65535),
			Protocol:        pulumi.String("all"),
			CidrBlocks:      pulumi.StringArray{pulumi.String("0.0.0.0/0")},
			SecurityGroupId: talosSg.ID(),
		})
		if err != nil {
			log.Printf("error adding rule to security group: %s", err.Error())
			return err
		}

		// Create a security group for the load balancer
		talosLbSg, err := ec2.NewSecurityGroup(ctx, "talosLbSg", &ec2.SecurityGroupArgs{
			Name:        pulumi.String("talosLbSg"),
			VpcId:       talosVpc.VpcId,
			Description: pulumi.String("Security group for the Talos load balancer"),
			Tags: pulumi.StringMap{
				"Name":  pulumi.String("talosLbSg"),
				"Owner": pulumi.String(ownerTagValue),
				"Team":  pulumi.String(teamTagValue),
			},
		})
		if err != nil {
			log.Printf("error creating security group: %s", err.Error())
			return err
		}

		// Allow K8s API inbound to load balancer
		_, err = ec2.NewSecurityGroupRule(ctx, "allowK8sApiLb", &ec2.SecurityGroupRuleArgs{
			Type:            pulumi.String("ingress"),
			FromPort:        pulumi.Int(6443),
			ToPort:          pulumi.Int(6443),
			Protocol:        pulumi.String("tcp"),
			CidrBlocks:      pulumi.StringArray{pulumi.String(inboundAllowedIPs)},
			SecurityGroupId: talosLbSg.ID(),
		})
		if err != nil {
			log.Printf("error adding rule to security group: %s", err.Error())
			return err
		}

		// Allow K8s API traffic outbound to nodes
		_, err = ec2.NewSecurityGroupRule(ctx, "allowEgressLb", &ec2.SecurityGroupRuleArgs{
			Type:            pulumi.String("egress"),
			FromPort:        pulumi.Int(6443),
			ToPort:          pulumi.Int(6443),
			Protocol:        pulumi.String("tcp"),
			CidrBlocks:      pulumi.StringArray{pulumi.String(vpcNetworkCidr)},
			SecurityGroupId: talosLbSg.ID(),
		})
		if err != nil {
			log.Printf("error adding rule to security group: %s", err.Error())
			return err
		}

		// Allow traffic from load balancer to Talos cluster
		_, err = ec2.NewSecurityGroupRule(ctx, "allowTalosLb", &ec2.SecurityGroupRuleArgs{
			Type:                  pulumi.String("ingress"),
			FromPort:              pulumi.Int(0),
			ToPort:                pulumi.Int(65535),
			Protocol:              pulumi.String("all"),
			SourceSecurityGroupId: talosLbSg.ID(),
			SecurityGroupId:       talosSg.ID(),
		})
		if err != nil {
			log.Printf("error adding rule to security group: %s", err.Error())
			return err
		}

		// Create a load balancer
		// Details: https://www.pulumi.com/registry/packages/aws/api-docs/elb/loadbalancer/
		talosLb, err := elb.NewLoadBalancer(ctx, "talosLb", &elb.LoadBalancerArgs{
			Name: pulumi.String("talosLb"),
			Listeners: elb.LoadBalancerListenerArray{
				&elb.LoadBalancerListenerArgs{
					InstancePort:     pulumi.Int(6443),
					InstanceProtocol: pulumi.String("tcp"),
					LbPort:           pulumi.Int(6443),
					LbProtocol:       pulumi.String("tcp"),
				},
			},
			SecurityGroups: pulumi.StringArray{talosLbSg.ID()},
			Subnets:        talosVpc.PublicSubnetIds,
			Tags: pulumi.StringMap{
				"Name":  pulumi.String("talosLb"),
				"Owner": pulumi.String(ownerTagValue),
				"Team":  pulumi.String(teamTagValue),
			},
		})
		if err != nil {
			log.Printf("error creating load balancer: %s", err.Error())
			return err
		}

		// Get ID for the official Talos Linux AMI
		// Details: https://www.pulumi.com/registry/packages/aws/api-docs/ec2/getami/
		talosAmi, err := ec2.LookupAmi(ctx, &ec2.LookupAmiArgs{
			Owners:     []string{"540036508848"},
			MostRecent: pulumi.BoolRef(true),
			Filters: []ec2.GetAmiFilter{
				{Name: "name", Values: []string{"talos-v1.11*"}},
				{Name: "root-device-type", Values: []string{"ebs"}},
				{Name: "virtualization-type", Values: []string{"hvm"}},
				{Name: "architecture", Values: []string{"x86_64"}},
			},
		})
		if err != nil {
			log.Printf("error looking up Talos Linux AMI: %s", err.Error())
		}

		// Launch EC2 instances for the control plane nodes
		// Details: https://www.pulumi.com/registry/packages/aws/api-docs/ec2/instance/
		cpInstanceIds := make([]pulumi.StringInput, 3)
		cpInstancePrivIps := make([]pulumi.StringInput, 3)
		cpInstancePubIps := make([]pulumi.StringInput, 3)
		for i := range 3 {
			instance, err := ec2.NewInstance(ctx, fmt.Sprintf("talosCp-0%d", i), &ec2.InstanceArgs{
				Ami:                      pulumi.String(talosAmi.Id),
				AssociatePublicIpAddress: pulumi.Bool(true),
				InstanceType:             pulumi.String("m5a.xlarge"),
				SubnetId:                 talosVpc.PublicSubnetIds.Index(pulumi.Int(i)),
				Tags: pulumi.StringMap{
					"Name":  pulumi.Sprintf("talosCp-0%d", i),
					"Owner": pulumi.String(ownerTagValue),
					"Team":  pulumi.String(teamTagValue),
				},
				VpcSecurityGroupIds: pulumi.StringArray{talosSg.ID()},
			})
			if err != nil {
				log.Printf("error creating instance: %s", err.Error())
			} else {
				cpInstanceIds[i] = instance.ID()
				cpInstancePrivIps[i] = instance.PrivateIp
				cpInstancePubIps[i] = instance.PublicIp
			}
		}

		// Attach control plane instances to load balancer
		// Details: https://www.pulumi.com/registry/packages/aws/api-docs/elb/attachment/
		for i := range 3 {
			_, err := elb.NewAttachment(ctx, fmt.Sprintf("lbAttachment-0%d", i), &elb.AttachmentArgs{
				Elb:      talosLb.ID(),
				Instance: cpInstanceIds[i],
			})
			if err != nil {
				log.Printf("error attaching instance to load balancer: %s", err.Error())
			}
		}

		// Build the Talos cluster configuration
		// First, generate machine secrets
		// Details: https://www.pulumi.com/registry/packages/talos/api-docs/machine/secrets/
		talosSecrets, err := machine.NewSecrets(ctx, "talos-secrets", nil)
		if err != nil {
			log.Printf("error generating machine secrets: %s", err.Error())
		}

		// Get machine configuration for the control plane
		// Details: https://www.pulumi.com/registry/packages/talos/api-docs/machine/getconfiguration/
		talosCpCfg := machine.GetConfigurationOutput(ctx, machine.GetConfigurationOutputArgs{
			ClusterEndpoint: pulumi.Sprintf("https://%v:6443", talosLb.DnsName),
			ClusterName:     pulumi.String("talos-cluster"),
			Docs:            pulumi.BoolPtr(false),
			Examples:        pulumi.BoolPtr(false),
			MachineSecrets: machine.MachineSecretsArgs{
				Certs:      talosSecrets.MachineSecrets.Certs(),
				Cluster:    talosSecrets.MachineSecrets.Cluster(),
				Secrets:    talosSecrets.MachineSecrets.Secrets(),
				Trustdinfo: talosSecrets.MachineSecrets.Trustdinfo(),
			},
			MachineType:  pulumi.String("controlplane"),
			TalosVersion: pulumi.String("v1.8"),
		})

		// Get machine configuration for the worker nodes
		talosWkrCfg := machine.GetConfigurationOutput(ctx, machine.GetConfigurationOutputArgs{
			ClusterEndpoint: pulumi.Sprintf("https://%v:6443", talosLb.DnsName),
			ClusterName:     pulumi.String("talos-cluster"),
			Docs:            pulumi.BoolPtr(false),
			Examples:        pulumi.BoolPtr(false),
			MachineSecrets: machine.MachineSecretsArgs{
				Certs:      talosSecrets.MachineSecrets.Certs(),
				Cluster:    talosSecrets.MachineSecrets.Cluster(),
				Secrets:    talosSecrets.MachineSecrets.Secrets(),
				Trustdinfo: talosSecrets.MachineSecrets.Trustdinfo(),
			},
			MachineType:  pulumi.String("worker"),
			TalosVersion: pulumi.String("v1.8"),
		})

		// Apply the machine configuration to the control plane nodes
		// Not using a loop here because we need to create a dependency on these resources
		// Details: https://www.pulumi.com/registry/packages/talos/api-docs/machine/configurationapply/
		cpConfigApply00, err := machine.NewConfigurationApply(ctx, "cpConfigApply-00", &machine.ConfigurationApplyArgs{
			ClientConfiguration: machine.ClientConfigurationArgs{
				CaCertificate:     talosSecrets.ClientConfiguration.CaCertificate(),
				ClientCertificate: talosSecrets.ClientConfiguration.ClientCertificate(),
				ClientKey:         talosSecrets.ClientConfiguration.ClientKey(),
			},
			MachineConfigurationInput: talosCpCfg.MachineConfiguration(),
			Node:                      cpInstancePubIps[0],
		})
		if err != nil {
			log.Printf("error applying machine configuration: %s", err.Error())
		}

		cpConfigApply01, err := machine.NewConfigurationApply(ctx, "cpConfigApply-01", &machine.ConfigurationApplyArgs{
			ClientConfiguration: machine.ClientConfigurationArgs{
				CaCertificate:     talosSecrets.ClientConfiguration.CaCertificate(),
				ClientCertificate: talosSecrets.ClientConfiguration.ClientCertificate(),
				ClientKey:         talosSecrets.ClientConfiguration.ClientKey(),
			},
			MachineConfigurationInput: talosCpCfg.MachineConfiguration(),
			Node:                      cpInstancePubIps[1],
		})
		if err != nil {
			log.Printf("error applying machine configuration: %s", err.Error())
		}

		cpConfigApply02, err := machine.NewConfigurationApply(ctx, "cpConfigApply-02", &machine.ConfigurationApplyArgs{
			ClientConfiguration: machine.ClientConfigurationArgs{
				CaCertificate:     talosSecrets.ClientConfiguration.CaCertificate(),
				ClientCertificate: talosSecrets.ClientConfiguration.ClientCertificate(),
				ClientKey:         talosSecrets.ClientConfiguration.ClientKey(),
			},
			MachineConfigurationInput: talosCpCfg.MachineConfiguration(),
			Node:                      cpInstancePubIps[2],
		})
		if err != nil {
			log.Printf("error applying machine configuration: %s", err.Error())
		}

		// Launch EC2 instances for the worker nodes
		// Details: https://www.pulumi.com/registry/packages/aws/api-docs/ec2/instance/
		wkrInstanceIds := make([]pulumi.StringInput, 3)
		wkrInstancePrivIps := make([]pulumi.StringInput, 3)
		wkrInstancePubIps := make([]pulumi.StringInput, 3)
		for i := range 3 {
			instance, err := ec2.NewInstance(ctx, fmt.Sprintf("talosWkr-0%d", i), &ec2.InstanceArgs{
				Ami:                      pulumi.String(talosAmi.Id),
				AssociatePublicIpAddress: pulumi.Bool(true),
				InstanceType:             pulumi.String("m5a.xlarge"),
				SubnetId:                 talosVpc.PublicSubnetIds.Index(pulumi.Int(i)),
				Tags: pulumi.StringMap{
					"Name":  pulumi.Sprintf("talosWkr-0%d", i),
					"Owner": pulumi.String(ownerTagValue),
					"Team":  pulumi.String(teamTagValue),
				},
				VpcSecurityGroupIds: pulumi.StringArray{talosSg.ID()},
			})
			if err != nil {
				log.Printf("error creating instance: %s", err.Error())
			} else {
				wkrInstanceIds[i] = instance.ID()
				wkrInstancePrivIps[i] = instance.PrivateIp
				wkrInstancePubIps[i] = instance.PublicIp
			}
		}

		// Apply the machine configuration to the worker nodes
		for i := range wkrInstancePubIps {
			_, err = machine.NewConfigurationApply(ctx, fmt.Sprintf("wkrConfigApply-0%d", i), &machine.ConfigurationApplyArgs{
				ClientConfiguration: machine.ClientConfigurationArgs{
					CaCertificate:     talosSecrets.ClientConfiguration.CaCertificate(),
					ClientCertificate: talosSecrets.ClientConfiguration.ClientCertificate(),
					ClientKey:         talosSecrets.ClientConfiguration.ClientKey(),
				},
				MachineConfigurationInput: talosWkrCfg.MachineConfiguration(),
				Node:                      wkrInstancePubIps[i],
			})
			if err != nil {
				log.Printf("error applying machine configuration: %s", err.Error())
			}
		}

		// Bootstrap the first control plane node
		// Details: https://www.pulumi.com/registry/packages/talos/api-docs/machine/bootstrap/
		_, err = machine.NewBootstrap(ctx, "bootstrap", &machine.BootstrapArgs{
			ClientConfiguration: machine.ClientConfigurationArgs{
				CaCertificate:     talosSecrets.ClientConfiguration.CaCertificate(),
				ClientCertificate: talosSecrets.ClientConfiguration.ClientCertificate(),
				ClientKey:         talosSecrets.ClientConfiguration.ClientKey(),
			},
			Node: cpInstancePubIps[0],
		}, pulumi.DependsOn([]pulumi.Resource{cpConfigApply00, cpConfigApply01, cpConfigApply02}))
		if err != nil {
			log.Printf("error bootstrapping first node: %s", err.Error())
		}

		// Get client configuration for the Talos cluster
		talosClusterClientCfg := client.GetConfigurationOutput(ctx, client.GetConfigurationOutputArgs{
			ClusterName: pulumi.String("talos-cluster"),
			ClientConfiguration: client.GetConfigurationClientConfigurationArgs{
				CaCertificate:     talosSecrets.ClientConfiguration.CaCertificate(),
				ClientCertificate: talosSecrets.ClientConfiguration.ClientCertificate(),
				ClientKey:         talosSecrets.ClientConfiguration.ClientKey(),
			},
			Nodes: pulumi.StringArray{
				cpInstancePubIps[0],
			},
			Endpoints: pulumi.StringArray{
				cpInstancePubIps[0],
			},
		})

		// Export the Talos client configuration
		ctx.Export("talosctlCfg", talosClusterClientCfg.TalosConfig())
		// Uncomment the following lines for additional outputs that may be useful for troubleshooting/diagnostics
		// ctx.Export("talosVpcId", talosVpc.VpcId)
		// ctx.Export("talosPrivSubnetIds", talosVpc.PrivateSubnetIds)
		// ctx.Export("talosPubSubnetIds", talosVpc.PublicSubnetIds)
		// ctx.Export("talosSgId", talosSg.ID())
		// ctx.Export("talosLbSgId", talosLbSg.ID())
		// ctx.Export("talosLbDnsName", talosLb.DnsName)
		// ctx.Export("talosLbArn", talosLb.Arn)
		// ctx.Export("talosLbId", talosLb.ID())
		// ctx.Export("cpInstanceIds", pulumi.StringArray(cpInstanceIds))
		// ctx.Export("cpInstancePrivIps", pulumi.StringArray(cpInstancePrivIps))
		ctx.Export("cpInstancePubIps", pulumi.StringArray(cpInstancePubIps))
		// ctx.Export("wkrInstanceIds", pulumi.StringArray(wkrInstanceIds))
		// ctx.Export("wkrInstancePrivIps", pulumi.StringArray(wkrInstancePrivIps))
		ctx.Export("wkrInstancePubIps", pulumi.StringArray(wkrInstancePubIps))

		return nil
	})
}
