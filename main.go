package main

import (
	"fmt"
	"log"

	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v5/go/aws/elb"
	awsx "github.com/pulumi/pulumi-awsx/sdk/go/awsx/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
	"github.com/siderolabs/pulumi-provider-talos/sdk/go/talos"
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

		// Create a new VPC, subnets, and associated infrastructure
		talosVpc, err := awsx.NewVpc(ctx, "talosVpc", &awsx.VpcArgs{
			EnableDnsHostnames: pulumi.Bool(true),
			EnableDnsSupport:   pulumi.Bool(true),
			CidrBlock:          &vpcNetworkCidr,
			NatGateways: &awsx.NatGatewayConfigurationArgs{
				Strategy: awsx.NatGatewayStrategySingle,
			},
			Tags: pulumi.StringMap{
				"Name": pulumi.String("talosVpc"),
			},
		})
		if err != nil {
			log.Printf("error creating security group: %s", err.Error())
			return err
		}
		ctx.Export("talosVpcId", talosVpc.VpcId)
		ctx.Export("talosPrivSubnetIds", talosVpc.PrivateSubnetIds)
		ctx.Export("talosPubSubnetIds", talosVpc.PublicSubnetIds)

		// Create a security group for the Talos cluster
		talosSg, err := ec2.NewSecurityGroup(ctx, "talosSg", &ec2.SecurityGroupArgs{
			Name:        pulumi.String("talosSg"),
			VpcId:       talosVpc.VpcId,
			Description: pulumi.String("Security group for the Talos cluster"),
			Tags: pulumi.StringMap{
				"Name": pulumi.String("talosSg"),
			},
		})
		if err != nil {
			log.Printf("error creating security group: %s", err.Error())
			return err
		}
		ctx.Export("talosSgId", talosSg.ID())

		// Add rules to Talos security group
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
				"Name": pulumi.String("talosLbSg"),
			},
		})
		if err != nil {
			log.Printf("error creating security group: %s", err.Error())
			return err
		}
		ctx.Export("talosLbSgId", talosLbSg.ID())

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
				"Name": pulumi.String("talosLb"),
			},
		})
		if err != nil {
			log.Printf("error creating load balancer: %s", err.Error())
			return err
		}
		ctx.Export("talosLbDnsName", talosLb.DnsName)
		ctx.Export("talosLbArn", talosLb.Arn)
		ctx.Export("talosLbId", talosLb.ID())

		// Launch EC2 instances for the control plane nodes
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
					"Name": pulumi.Sprintf("talosCp-0%d", i),
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
		ctx.Export("cpInstanceIds", pulumi.StringArray(cpInstanceIds))
		ctx.Export("cpInstancePrivIps", pulumi.StringArray(cpInstancePrivIps))
		ctx.Export("cpInstancePubIps", pulumi.StringArray(cpInstancePubIps))

		// Attach control plane instances to load balancer
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
		talosMachineSecrets, err := talos.NewTalosMachineSecrets(ctx, "talosMs", nil)
		if err != nil {
			log.Printf("error generating machine secrets: %s", err.Error())
		}
		talosCfg, err := talos.NewTalosClientConfiguration(ctx, "talosCfg", &talos.TalosClientConfigurationArgs{
			ClusterName:    pulumi.String("talos-cluster"),
			MachineSecrets: talosMachineSecrets.MachineSecrets,
			Endpoints:      pulumi.StringArray(cpInstancePubIps),
			Nodes:          pulumi.StringArray{cpInstancePubIps[0]},
		})
		if err != nil {
			log.Printf("error creating client configuration: %s", err.Error())
		}
		talosCpMachineCfg, err := talos.NewTalosMachineConfigurationControlplane(ctx, "cpMachineCfg", &talos.TalosMachineConfigurationControlplaneArgs{
			ClusterName:     talosCfg.ClusterName,
			ClusterEndpoint: pulumi.Sprintf("https://%v:6443", talosLb.DnsName),
			MachineSecrets:  talosMachineSecrets.MachineSecrets,
			DocsEnabled:     pulumi.Bool(false),
			ExamplesEnabled: pulumi.Bool(false),
		})
		if err != nil {
			log.Printf("error creating control plane configuration: %s", err.Error())
		}
		talosWkrMachineCfg, err := talos.NewTalosMachineConfigurationWorker(ctx, "wkrMachineCfg", &talos.TalosMachineConfigurationWorkerArgs{
			ClusterName:     talosCfg.ClusterName,
			ClusterEndpoint: pulumi.Sprintf("https://%v:6443", talosLb.DnsName),
			MachineSecrets:  talosMachineSecrets.MachineSecrets,
			DocsEnabled:     pulumi.Bool(false),
			ExamplesEnabled: pulumi.Bool(false),
		})
		if err != nil {
			log.Printf("error creating worker configuration: %s", err.Error())
		}

		// Apply the machine configuration to the control plane nodes
		for i := 0; i < len(cpInstancePubIps); i++ {
			_, err = talos.NewTalosMachineConfigurationApply(ctx, fmt.Sprintf("cpConfigApply-0%d", i), &talos.TalosMachineConfigurationApplyArgs{
				TalosConfig:          talosCfg.TalosConfig,
				MachineConfiguration: talosCpMachineCfg.MachineConfig,
				Endpoint:             cpInstancePubIps[i],
				Node:                 cpInstancePubIps[i],
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
					"Name": pulumi.Sprintf("talosWkr-0%d", i),
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
		ctx.Export("wkrInstanceIds", pulumi.StringArray(wkrInstanceIds))
		ctx.Export("wkrInstancePrivIps", pulumi.StringArray(wkrInstancePrivIps))
		ctx.Export("wkrInstancePubIps", pulumi.StringArray(wkrInstancePubIps))

		// Apply the machine configuration to the worker nodes
		for i := 0; i < len(wkrInstancePubIps); i++ {
			_, err = talos.NewTalosMachineConfigurationApply(ctx, fmt.Sprintf("wkrConfigApply-0%d", i), &talos.TalosMachineConfigurationApplyArgs{
				TalosConfig:          talosCfg.TalosConfig,
				MachineConfiguration: talosWkrMachineCfg.MachineConfig,
				Endpoint:             wkrInstancePubIps[i],
				Node:                 wkrInstancePubIps[i],
			})
			if err != nil {
				log.Printf("error applying machine configuration: %s", err.Error())
			}
		}

		// Bootstrap the first control plane node
		_, err = talos.NewTalosMachineBootstrap(ctx, "bootstrap", &talos.TalosMachineBootstrapArgs{
			TalosConfig: talosCfg.TalosConfig,
			Endpoint:    cpInstancePubIps[0],
			Node:        cpInstancePubIps[0],
		})
		if err != nil {
			log.Printf("error bootstrapping first node: %s", err.Error())
		}

		// Export the Talos client configuration
		ctx.Export("talosctlCfg", talosCfg.TalosConfig)

		return nil
	})
}
