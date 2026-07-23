package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: docker-utils [global options] <command> [options]\n\n")
	fmt.Fprintf(os.Stderr, "Global Options:\n")
	fmt.Fprintf(os.Stderr, "  -v  Enable verbose output\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  disable-always-restart  Update containers with restart policy 'always' to 'unless-stopped'\n")
}

func main() {
	rootCmd := flag.NewFlagSet("docker-utils", flag.ExitOnError)
	rootCmd.Usage = printUsage
	globalVerbose := rootCmd.Bool("v", false, "Enable verbose output")
	rootCmd.Parse(os.Args[1:])

	args := rootCmd.Args()
	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "disable-always-restart":
		cmd := flag.NewFlagSet("disable-always-restart", flag.ExitOnError)
		force := cmd.Bool("f", false, "Actually apply changes")
		verbose := cmd.Bool("v", false, "Enable verbose output")
		cmd.Parse(args[1:])

		isVerbose := *globalVerbose || *verbose
		disableAlwaysRestart(*force, isVerbose)

	case "help":
		printUsage()
		os.Exit(0)

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", args[0])
		printUsage()
		os.Exit(1)
	}
}

func disableAlwaysRestart(force, verbose bool) {
	ctx := context.Background()

	cli, err := client.New(client.FromEnv)
	if err != nil {
		log.Fatalf("Error creating Docker client: %v", err)
	}
	defer cli.Close()

	listOptions := client.ContainerListOptions{
		All: true,
	}

	result, err := cli.ContainerList(ctx, listOptions)
	if err != nil {
		log.Fatalf("Error listing containers: %v", err)
	}

	if !force {
		fmt.Println("DRY-RUN MODE ACTIVE: No changes will be saved to Docker, use -f to apply changes.")
	}
	if verbose {
		fmt.Printf("Scanning %d running containers...\n\n", len(result.Items))
	}
	updatedCount := 0

	for _, c := range result.Items {
		inspect, err := cli.ContainerInspect(ctx, c.ID, client.ContainerInspectOptions{})
		if err != nil {
			log.Printf("Warning: Could not inspect container %s: %v", c.ID[:12], err)
			continue
		}

		currentPolicy := inspect.Container.HostConfig.RestartPolicy.Name
		containerName := c.Names[0]

		if inspect.Container.HostConfig.RestartPolicy.IsAlways() {
			if !force {
				if verbose {
					fmt.Printf("[DRY-RUN] Would update: %s (always -> unless-stopped)\n", containerName)
				}
				updatedCount++
			} else {
				if verbose {
					fmt.Printf("Updating: %s... ", containerName)
				}

				updateOptions := client.ContainerUpdateOptions{
					RestartPolicy: &container.RestartPolicy{
						Name: container.RestartPolicyUnlessStopped,
					},
				}

				_, err = cli.ContainerUpdate(ctx, c.ID, updateOptions)
				if err != nil {
					if verbose {
						fmt.Printf("Error: %v\n", err)
					}
					log.Printf("Error updating container %s: %v", containerName, err)
				} else {
					if verbose {
						fmt.Println("Successfully set to 'unless-stopped'")
					}
					updatedCount++
				}
			}
		} else {
			if verbose {
				fmt.Printf("[VERBOSE] Skipping: %s (Current policy is '%s', not 'always')\n", containerName, currentPolicy)
			}
		}
	}

	if verbose {
		if !force {
			fmt.Printf("Scan complete. Would have updated %d container(s).\n", updatedCount)
		} else {
			fmt.Printf("Scan complete. Successfully updated %d container(s).\n", updatedCount)
		}
	}
}

