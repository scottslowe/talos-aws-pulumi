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
			vpcNetworkCidr = "10.0.0.0/16"
		}
		talosAmi := cfg.Require("talosAmi")
		ownerTagValue, err := config.Try(ctx, "ownerTagValue")
		if err != nil {
			ownerTagValue = "nobody@nowhere.com"
		}
		teamTagValue, err := config.Try(ctx, "teamTagValue")
		if err != nil {
			teamTagValue = "TeamOfOne"
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
			CidrBlocks:      pulumi.StringArray{pulumi.String("0.0.0.0/0")},
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
			ToPort:          pulumi.Int(50001),
			Protocol:        pulumi.String("tcp"),
			CidrBlocks:      pulumi.StringArray{pulumi.String("0.0.0.0/0")},
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
			CidrBlocks:      pulumi.StringArray{pulumi.String("0.0.0.0/0")},
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

		// Launch EC2 instances for the control plane nodes
		// Details: https://www.pulumi.com/registry/packages/aws/api-docs/ec2/instance/
		cpInstanceIds := make([]pulumi.StringInput, 3)
		cpInstancePrivIps := make([]pulumi.StringInput, 3)
		cpInstancePubIps := make([]pulumi.StringInput, 3)
		for i := 0; i < 3; i++ {
			instance, err := ec2.NewInstance(ctx, fmt.Sprintf("talosCp-0%d", i), &ec2.InstanceArgs{
				Ami:                      pulumi.String(talosAmi),
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
		for i := 0; i < 3; i++ {
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
		// Details: TODO FIX URL
		// talosMachineSecrets, err := machine.NewSecrets(ctx, "talosMs", nil)
		// if err != nil {
		// 	log.Printf("error generating machine secrets: %s", err.Error())
		// }

		talosSecrets, err := machine.NewSecretsType(ctx, "talos-secrets", nil)

		// Get machine configuration for the control plane
		// Details: TODO FIX URL
		talosCpCfg := machine.GetConfigurationOutput(ctx, machine.GetConfigurationOutputArgs{
			ClusterEndpoint: pulumi.Sprintf("https://%v:6443", talosLb.DnsName),
			ClusterName:     pulumi.String("talos-cluster"),
			Docs:            pulumi.BoolPtr(false),
			Examples:        pulumi.BoolPtr(false),
			// MachineSecrets:  talosMachineSecrets.MachineSecrets,
			MachineSecrets: talosSecrets.ToSecretsTypeOutput().MachineSecrets(),
			MachineType:    pulumi.String("controlplane"),
			TalosVersion:   pulumi.String("v1.6"),
		})

		// Get machine configuration for the worker nodes
		// Details: TODO FIX URL
		talosWkrCfg := machine.GetConfigurationOutput(ctx, machine.GetConfigurationOutputArgs{
			ClusterEndpoint: pulumi.Sprintf("https://%v:6443", talosLb.DnsName),
			ClusterName:     pulumi.String("talos-cluster"),
			Docs:            pulumi.BoolPtr(false),
			Examples:        pulumi.BoolPtr(false),
			// MachineSecrets:  talosMachineSecrets.MachineSecrets,
			MachineSecrets: talosSecrets.ToSecretsTypeOutput().MachineSecrets(),
			MachineType:    pulumi.String("worker"),
			TalosVersion:   pulumi.String("v1.6"),
		})

		// Apply the machine configuration to the control plane nodes
		// Details: TODO FIX URL
		for i := 0; i < len(cpInstancePubIps); i++ {
			_, err = machine.NewConfigurationApply(ctx, fmt.Sprintf("cpConfigApply-0%d", i), &machine.ConfigurationApplyArgs{
				// ClientConfiguration:       talosMachineSecrets.ClientConfiguration,
				ClientConfiguration:       talosSecrets.ToSecretsTypeOutput().ClientConfiguration(),
				MachineConfigurationInput: talosCpCfg.MachineConfiguration(),
				Node:                      cpInstancePubIps[i],
			})
			if err != nil {
				log.Printf("error applying machine configuration: %s", err.Error())
			}
		}

		// Launch EC2 instances for the worker nodes
		wkrInstanceIds := make([]pulumi.StringInput, 3)
		wkrInstancePrivIps := make([]pulumi.StringInput, 3)
		wkrInstancePubIps := make([]pulumi.StringInput, 3)
		for i := 0; i < 3; i++ {
			instance, err := ec2.NewInstance(ctx, fmt.Sprintf("talosWkr-0%d", i), &ec2.InstanceArgs{
				Ami:                      pulumi.String(talosAmi),
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
		for i := 0; i < len(wkrInstancePubIps); i++ {
			_, err = machine.NewConfigurationApply(ctx, fmt.Sprintf("wkrConfigApply-0%d", i), &machine.ConfigurationApplyArgs{
				// ClientConfiguration:       talosMachineSecrets.ClientConfiguration,
				ClientConfiguration:       talosSecrets.ToSecretsTypeOutput().ClientConfiguration(),
				MachineConfigurationInput: talosWkrCfg.MachineConfiguration(),
				Node:                      wkrInstancePubIps[i],
			})
			if err != nil {
				log.Printf("error applying machine configuration: %s", err.Error())
			}
		}

		// Bootstrap the first control plane node
		// Details: TODO FIX URL
		_, err = machine.NewBootstrap(ctx, "bootstrap", &machine.BootstrapArgs{
			// ClientConfiguration: talosMachineSecrets.ClientConfiguration,
			ClientConfiguration: talosSecrets.ToSecretsTypeOutput().ClientConfiguration(),
			Node:                cpInstancePubIps[0],
		})
		if err != nil {
			log.Printf("error bootstrapping first node: %s", err.Error())
		}

		// Get client configuration for the Talos cluster
		talosClusterClientCfg := client.GetConfigurationOutput(ctx, client.GetConfigurationOutputArgs{
			ClusterName: pulumi.String("talos-cluster"),
			// ClientConfiguration: talosMachineSecrets.ClientConfiguration,
			ClientConfiguration: talosSecrets.ToSecretsTypeOutput().ClientConfiguration(),
			Nodes: pulumi.StringArray{
				cpInstancePubIps[0],
			},
		})

		// Export the Talos client configuration
		ctx.Export("talosctlCfg", talosClusterClientCfg.ClientConfiguration())

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
		// ctx.Export("cpInstancePubIps", pulumi.StringArray(cpInstancePubIps))
		// ctx.Export("wkrInstanceIds", pulumi.StringArray(wkrInstanceIds))
		// ctx.Export("wkrInstancePrivIps", pulumi.StringArray(wkrInstancePrivIps))
		// ctx.Export("wkrInstancePubIps", pulumi.StringArray(wkrInstancePubIps))

		return nil
	})
}
