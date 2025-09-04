# Standing up a Talos Linux Cluster on AWS Using Pulumi

This repository contains a [Pulumi](https://www.pulumi.com) program, written in Golang, to automate the process of standing up a [Talos Linux](https://talos.dev) cluster on AWS.

## Prerequisites

Before using the contents of this repository, you will need to ensure:

* You have the Pulumi CLI installed (see [here](https://www.pulumi.com/docs/get-started/install/) for more information on installing Pulumi).
* You have a working AWS CLI installation.
* You have a working installation of Golang.
* You have installed the `talosctl` command-line tool (see [the Talos GitHub repository](https://github.com/siderolabs/talos/) to get the `talosctl` command-line tool).

## Instructions

1. Clone this repository into a directory on your local computer.

2. Change into the directory where you cloned this repository.

3. Run `pulumi stack init` to create a new Pulumi stack.

4. Use `pulumi config set aws:region <region>` to set the desired AWS region in which to create the cluster. (Depending on the configuration of your AWS CLI, you may be able to omit this step.)

5. Run `pulumi up` to run the Pulumi program.

After the Pulumi program finishes running, you can obtain a configuration file for `talosctl` using this command:

```shell
pulumi stack output talosctlCfg --show-secrets > talosconfig
```

If you don't want to have to explicitly reference the Talos configuration file in subsequent commands, put the output of the above command in `$HOME/.talos/config`. The command above writes it to a file named `talosconfig` in the current working directory.

You can then run this command to watch the cluster bootstrap:

```shell
talosctl --talosconfig talosconfig health
```

Once the cluster has finished boostrapping, you can retrieve the Kubeconfig necessary to access the cluster with this command:

```shell
talosctl --talosconfig talosconfig kubeconfig
```

By default, this command merges the Kubeconfig for this cluster into the default Kubeconfig file (`$HOME/.kube/config`). You can append a filename to the command to have it written to a different file.

From this point, you can use `kubectl` to access the cluster as normal, referencing the recently-retrieved Kubeconfig as necessary.
