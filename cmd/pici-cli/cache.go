package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/Thiht/pici/client"
)

func newCacheCmd(c *client.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache <project>",
		Short: "Show a project's Docker cache (images and volumes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := applyFormat(cmd); err != nil {
				return err
			}
			cache, err := c.ProjectCache(context.Background(), args[0])
			if err != nil {
				return err
			}
			return output(cache)
		},
	}
	cmd.ValidArgsFunction = func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return projectNames(c), cobra.ShellCompDirectiveNoFileComp
	}
	cmd.AddCommand(newCacheRmImageCmd(c), newCacheRmVolumeCmd(c))
	return cmd
}

func newCacheRmImageCmd(c *client.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rm-image <project> <reference>",
		Short: "Remove a cached workflow image",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return c.DeleteCacheImage(context.Background(), args[0], args[1])
		},
	}
	cmd.ValidArgsFunction = func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return projectNames(c), cobra.ShellCompDirectiveNoFileComp
		}
		if len(args) == 1 {
			return cacheNames(c, args[0], true), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return cmd
}

func newCacheRmVolumeCmd(c *client.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rm-volume <project> <name>",
		Short: "Remove a cross-run cache volume",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return c.DeleteCacheVolume(context.Background(), args[0], args[1])
		},
	}
	cmd.ValidArgsFunction = func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return projectNames(c), cobra.ShellCompDirectiveNoFileComp
		}
		if len(args) == 1 {
			return cacheNames(c, args[0], false), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return cmd
}

func cacheNames(c *client.Client, project string, images bool) []string {
	cache, err := c.ProjectCache(context.Background(), project)
	if err != nil {
		return nil
	}
	if images {
		names := make([]string, 0, len(cache.Images))
		for _, img := range cache.Images {
			names = append(names, img.Reference)
		}
		return names
	}
	names := make([]string, 0, len(cache.Volumes))
	for _, v := range cache.Volumes {
		names = append(names, v.Name)
	}
	return names
}
