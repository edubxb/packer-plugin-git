// Package repository contains logic for providing repo data to Packer
//
//go:generate packer-sdc mapstructure-to-hcl2 -type Config,DatasourceOutput
package repository

import (
	"fmt"
	"log"
	"sort"

	"github.com/ethanmdavidson/packer-plugin-git/common"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/hashicorp/go-version"
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/packer-plugin-sdk/hcl2helper"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/zclconf/go-cty/cty"
)

const (
	TagsFilterSemVer = "SemVer"
)

type Config struct {
	Path       string `mapstructure:"path"`
	TagsFilter string `mapstructure:"tags_filter"`
}

type Datasource struct {
	config Config
}

type DatasourceOutput struct {
	Head     string   `mapstructure:"head"`
	Branches []string `mapstructure:"branches"`
	Tags     []string `mapstructure:"tags"`
	IsClean  bool     `mapstructure:"is_clean"`
}

func (d *Datasource) ConfigSpec() hcldec.ObjectSpec {
	return d.config.FlatMapstructure().HCL2Spec()
}

func (d *Datasource) Configure(raws ...interface{}) error {
	err := config.Decode(&d.config, nil, raws...)
	if err != nil {
		return err
	}
	if d.config.Path == "" {
		d.config.Path = "."
	}
	if d.config.TagsFilter != "" && d.config.TagsFilter != TagsFilterSemVer {
		return fmt.Errorf(
			"invalid tags_filter value: %q. Valid values are [%q]",
			d.config.TagsFilter,
			TagsFilterSemVer,
		)
	}

	return nil
}

func (d *Datasource) OutputSpec() hcldec.ObjectSpec {
	return (&DatasourceOutput{}).FlatMapstructure().HCL2Spec()
}

func (d *Datasource) Execute() (cty.Value, error) {
	log.Println("Starting execution")
	output := DatasourceOutput{}
	emptyOutput := hcl2helper.HCL2ValueFromConfig(output, d.OutputSpec())

	common.PrintOpeningRepo(d.config.Path)
	openOptions := &git.PlainOpenOptions{DetectDotGit: true}
	repo, err := git.PlainOpenWithOptions(d.config.Path, openOptions)
	if err != nil {
		return emptyOutput, err
	}
	log.Println("Repo opened")

	head, err := repo.Head()
	if err != nil {
		return emptyOutput, err
	}
	log.Printf("Head found: '%s'\n", head.String())

	worktree, err := repo.Worktree()
	if err != nil {
		return emptyOutput, err
	}
	log.Println("Worktree found")

	status, err := worktree.Status()
	if err != nil {
		return emptyOutput, err
	}
	log.Printf("Worktree status found: '%s'\n", status.String())

	branchIter, err := repo.Branches()
	if err != nil {
		return emptyOutput, err
	}
	log.Println("Branches found")

	tagIter, err := repo.Tags()
	if err != nil {
		return emptyOutput, err
	}
	log.Println("Tags found")

	output.Head = head.Name().Short()
	log.Printf("output.Head: '%s'\n", output.Head)

	output.IsClean = status.IsClean()
	log.Printf("output.IsClean: '%t'\n", output.IsClean)

	output.Branches = make([]string, 0)
	_ = branchIter.ForEach(func(reference *plumbing.Reference) error {
		log.Printf("Adding branch: '%s'\n", reference.Name().Short())
		output.Branches = append(output.Branches, reference.Name().Short())
		return nil
	})
	log.Printf("len(output.Branches): '%d'\n", len(output.Branches))

	allTags := make([]string, 0)
	_ = tagIter.ForEach(func(reference *plumbing.Reference) error {
		tagName := reference.Name().Short()
		log.Printf("Adding tag: '%s'\n", tagName)
		allTags = append(allTags, tagName)
		return nil
	})

	output.Tags = make([]string, 0)
	if d.config.TagsFilter == TagsFilterSemVer {
		log.Printf("Filtering SemVer compliant tags...")
		output.Tags = d.filterSemverTags(allTags)
	} else {
		output.Tags = allTags
	}
	log.Printf("len(output.Tags): '%d'\n", len(output.Tags))

	return hcl2helper.HCL2ValueFromConfig(output, d.OutputSpec()), nil
}

func (d *Datasource) filterSemverTags(tags []string) []string {
	semverTags := make([]*version.Version, 0)
	for _, t := range tags {
		v, err := version.NewVersion(t)
		if err == nil {
			log.Printf("Keeping SemVer compliant tag: '%s'\n", t)
			semverTags = append(semverTags, v)
		} else {
			log.Printf("Dropping non-SemVer compliant tag: '%s'\n", t)
		}
	}

	sortedTags := make([]string, len(semverTags))
	if len(semverTags) > 0 {
		sort.Sort(version.Collection(semverTags))
		for i, v := range semverTags {
			sortedTags[i] = v.Original()
		}
		log.Printf("Sorted SemVer Tags: %s", sortedTags)
	}

	return sortedTags
}
