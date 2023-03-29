# Standing up a Talos Linux Cluster on AWS Using Pulumi

This repository contains a [Pulumi](https://www.pulumi.com) program, written in Golang, to automate the process of standing up a [Talos Linux](https://talos.dev) cluster on AWS.

## Prerequisites

Before using the contents of this repository, you will need to ensure:

* You have the Pulumi CLI installed (see [here](https://www.pulumi.com/docs/get-started/install/) for more information on installing Pulumi).
* You have a working AWS CLI installation.
* You have manually installed the Pulumi provider for Talos. As of this writing, the Pulumi provider for Talos was still prerelease and needs to be installed manually; see instructions [here](https://blog.scottlowe.org/2023/02/08/installing-prerelease-pulumi-provider-talos/).

## Instructions

1. Clone this repository into a directory on your local computer.

2. Change into the directory where you cloned this repository.

3. Run `pulumi stack init` to create a new Pulumi stack.

4. Use `pulumi config set aws:region <region>` to set the desired AWS region in which to create the cluster.

5. Use `pulumi config set` to set the correct AMI ID for a Talos Linux instance in the desired AWS region. Information on determining the correct AMI ID can be found [here](https://www.talos.dev/v1.3/talos-guides/install/cloud-platforms/aws/#official-ami-images) in the Talos Linux documentation.

6. Run `pulumi up` to run the Pulumi program.

After the Pulumi program finishes running, you can obtain a configuration file for `talosctl` using this command:

```shell
pulumi stack output talosctlCfg --show-secrets > talosconfig
```

You can then run this command to watch the cluster bootstrap:

```shell
talosctl --talosconfig talosconfig health
```

Once the cluster has finished boostrapping, you can retrieve the Kubeconfig necessary to access the cluster with this command:

```shell
talosctl --talosconfig talosconfig kubeconfig
```

You can then use `kubectl` to access the cluster as normal, referencing the recently-retrieved Kubeconfig as necessary.
