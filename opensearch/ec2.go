package opensearch

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// userDataScript is a cloud-init script executed on first boot.
// It installs Docker, tunes the kernel for OpenSearch, and starts
// a single-node OpenSearch container with strict memory limits.
//
// Memory rationale for t3.micro (1 GiB RAM):
//   - The JVM heap is capped at 256 MiB (-Xms256m -Xmx256m).
//     OpenSearch's off-heap overhead (page cache, netty buffers, etc.)
//     adds roughly another 256 MiB, keeping total container RSS well
//     below 700 MiB and leaving headroom for the OS and Docker daemon.
//     Without these caps OpenSearch would default to 50 % of host RAM
//     (~512 MiB heap), causing the instance to swap-thrash or OOM-kill
//     the process within minutes.
const userDataScript = `#!/bin/bash
set -euo pipefail

# ---------------------------------------------------------------------------
# 1. Kernel tuning — required by OpenSearch / Elasticsearch
#    vm.max_map_count controls the maximum number of memory-map areas a
#    process may have. OpenSearch uses mmap for its Lucene index files;
#    without this the container refuses to start.
# ---------------------------------------------------------------------------
sysctl -w vm.max_map_count=262144
echo "vm.max_map_count=262144" >> /etc/sysctl.d/99-opensearch.conf

# ---------------------------------------------------------------------------
# 2. Install Docker (Amazon Linux 2023 ships dnf, not yum)
# ---------------------------------------------------------------------------
dnf update -y
dnf install -y docker
systemctl enable --now docker

# ---------------------------------------------------------------------------
# 3. Launch OpenSearch in a single-node Docker container
#
#    Key environment variables:
#      OPENSEARCH_JAVA_OPTS   — caps JVM heap at 256 MiB.  Keeping Xms==Xmx
#                               avoids expensive heap-resize GC pauses and
#                               prevents the JVM from grabbing more RAM at
#                               runtime, which is critical on a 1 GiB host.
#      DISABLE_SECURITY_PLUGIN — skips TLS negotiation and the internal user
#                               database, saving ~80-120 MiB of RAM that the
#                               security plugin would otherwise consume.
#      discovery.type         — tells OpenSearch not to wait for peer nodes,
#                               eliminating cluster-formation timeout loops.
# ---------------------------------------------------------------------------
docker run -d \
  --name opensearch \
  --restart unless-stopped \
  -p 9200:9200 \
  -p 9600:9600 \
  -e "OPENSEARCH_JAVA_OPTS=-Xms256m -Xmx256m" \
  -e "DISABLE_SECURITY_PLUGIN=true" \
  -e "discovery.type=single-node" \
  opensearchproject/opensearch:latest
`

type Resources struct {
	Instance      *ec2.Instance
	SecurityGroup *ec2.SecurityGroup
}

func Deploy(ctx *pulumi.Context, env string) (*Resources, error) {
	sg, err := createSecurityGroup(ctx, env)
	if err != nil {
		return nil, err
	}

	instance, err := createInstance(ctx, env, sg)
	if err != nil {
		return nil, err
	}

	return &Resources{
		Instance:      instance,
		SecurityGroup: sg,
	}, nil
}

func createSecurityGroup(ctx *pulumi.Context, env string) (*ec2.SecurityGroup, error) {
	return ec2.NewSecurityGroup(ctx, "opensearch-sg", &ec2.SecurityGroupArgs{
		Name:        pulumi.Sprintf("opensearch-sg-%s", env),
		Description: pulumi.String("Allow SSH and OpenSearch API access"),
		Tags: pulumi.StringMap{
			"Name":        pulumi.Sprintf("opensearch-sg-%s", env),
			"Environment": pulumi.String(env),
			"ManagedBy":   pulumi.String("pulumi"),
		},

		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Description: pulumi.String("SSH"),
				FromPort:    pulumi.Int(22),
				ToPort:      pulumi.Int(22),
				Protocol:    pulumi.String("tcp"),
				CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
			},

			&ec2.SecurityGroupIngressArgs{
				Description: pulumi.String("OpenSearch REST API"),
				FromPort:    pulumi.Int(9200),
				ToPort:      pulumi.Int(9200),
				Protocol:    pulumi.String("tcp"),
				CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
			},
		},

		Egress: ec2.SecurityGroupEgressArray{
			&ec2.SecurityGroupEgressArgs{
				Description: pulumi.String("Allow all outbound traffic"),
				FromPort:    pulumi.Int(0),
				ToPort:      pulumi.Int(0),
				Protocol:    pulumi.String("-1"),
				CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
			},
		},
	})
}

func createInstance(ctx *pulumi.Context, env string, sg *ec2.SecurityGroup) (*ec2.Instance, error) {
	ami, err := ec2.LookupAmi(ctx, &ec2.LookupAmiArgs{
		MostRecent: pulumi.BoolRef(true),
		Owners:     []string{"amazon"},
		Filters: []ec2.GetAmiFilter{
			{
				Name:   "name",
				Values: []string{"al2023-ami-*-x86_64"},
			},
			{
				Name:   "virtualization-type",
				Values: []string{"hvm"},
			},
			{
				Name:   "architecture",
				Values: []string{"x86_64"},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	return ec2.NewInstance(ctx, "opensearch-instance", &ec2.InstanceArgs{
		Ami:          pulumi.String(ami.ImageId),
		InstanceType: pulumi.String("t3.micro"),

		VpcSecurityGroupIds: pulumi.StringArray{sg.ID()},

		UserData: pulumi.String(userDataScript),

		RootBlockDevice: &ec2.InstanceRootBlockDeviceArgs{
			VolumeType: pulumi.String("gp3"),
			// The AL2023 AMI snapshot requires >= 30 GiB; 30 GiB is also the
			// exact Free Tier EBS allowance, so this is both the floor and ceiling.
			VolumeSize:          pulumi.Int(30),
			DeleteOnTermination: pulumi.Bool(true),
		},

		Tags: pulumi.StringMap{
			"Name":        pulumi.Sprintf("opensearch-%s", env),
			"Environment": pulumi.String(env),
			"ManagedBy":   pulumi.String("pulumi"),
		},
	})
}
