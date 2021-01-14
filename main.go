package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

type PackagePlusExtras struct {
	build.Package
	Deps []string
}

func GetCwd() string {
	path, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return path
}
func isGoAvailable() bool {
	cmd := exec.Command("/bin/sh", "-c", "command -v ", "go")
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

func isGitAvailable() bool {
	cmd := exec.Command("/bin/sh", "-c", "command -v ", "git")
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}
func goGit(ss ...string) []string {
	cmd := exec.Command("git", ss...)
	cmd.Dir = GetCwd()
	data, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), fmt.Sprintln())
}
func goTest(ss ...string) {
	fmt.Println("go", "test", ss)
	cmd := exec.Command("go", append([]string{"test"}, ss...)...)
	cmd.Stdout = os.Stdout
	cmd.Dir = GetCwd()

	err := cmd.Run()
	if err != nil {
		panic(err)
	}
	return
}
func goList(ss ...string) ([]*PackagePlusExtras, error) {
	r := append([]string{"list", "-f", "{}", "-json"}, ss...)
	//fmt.Println("go", r)
	cmd := exec.Command("go", r...)
	data, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	ret := []*PackagePlusExtras{}
	for decoder.More() {
		p := &PackagePlusExtras{}
		if err := decoder.Decode(p); err != nil {
			return nil, err
		}
		ret = append(ret, p)
	}
	return ret, nil
}
func GetCurrentPackage() string {
	rs, err := goList()
	if err != nil {
		panic(err)
	}
	return rs[0].ImportPath
}
func GetGitChangedFiles(branch string) []string {
	r := goGit("diff", goGit("merge-base", "--fork-point", branch)[0], "--name-only")

	if len(r) == 0 {
		fmt.Println("changed files:")
		fmt.Println(r)
		fmt.Println()
	}
	return r
}
func hash(aa []string, bb []string) []string {
	set := make([]string, 0)
	hash := make(map[string]bool)
	for _, a := range aa {
		hash[a] = true
	}

	for _, b := range bb {
		if _, ok := hash[b]; ok {
			set = append(set, b)
		}
	}
	return set
}

func init() {
	// cmd.PersistentFlags(). &packageVar, "head", "", "help message for head")
	// cmd. &packageVar, "entrypoints", "", "go file(s) used to build the binary")
	cmd.Flags().String("head", "", "head is the base of the git branch in which your incremental changes were made (master, dev)")
	cmd.MarkFlagRequired("head")
	cmd.Flags().StringSlice("entrypoints", []string{}, "entrypoints are the main packages that you will use to build your go binaries ")
	cmd.MarkFlagRequired("entrypoints")
}

var lock = sync.Mutex{}
var packagesToTest = []string{}
var seenPackages = map[string]bool{}

func walkPackages(base string, packages []*PackagePlusExtras, changedPackages []string) {
	next := []string{}
	for _, p := range packages {
		imports := p.Imports
		for _, imp := range imports {
			if _, seen := seenPackages[imp]; strings.Contains(imp, base) && !seen {
				next = append(next, imp)
			}
		}
	}
	if len(next) == 0 {
		return
	}
	t1, err := goList(next...)
	if err != nil {
		panic(err)
	}
	for _, t := range t1 {
		if r := hash(t.Deps, changedPackages); len(r) > 0 {
			fmt.Printf("package %s imports %v\n", t.ImportPath, r)
			packagesToTest = append(packagesToTest, t.ImportPath)
		}
		seenPackages[t.ImportPath] = true
	}
	walkPackages(base, t1, changedPackages)
}

var cmd = cobra.Command{
	Short: "test",
	Run: func(cmd *cobra.Command, args []string) {
		head, _ := cmd.Flags().GetString("head")
		entrypoints, _ := cmd.Flags().GetStringSlice("entrypoints")

		mainPackage := GetCurrentPackage()

		// need to test all packages in this folder if they are dependent on the
		// changed files. Convert the changed files into their respective packages (folders)
		gitFiles := GetGitChangedFiles(head)
		changedDirectories := map[string]bool{}
		for _, f := range gitFiles {
			changedDirectories[path.Join(mainPackage, filepath.Dir(f))] = true
		}
		// Convert from a hashmap to a list to pass parameters
		changedDirVar := []string{}
		for f := range changedDirectories {
			changedDirVar = append(changedDirVar, f)
		}
		changedP, err := goList(changedDirVar...)
		if err != nil {
			panic(err)
		}
		changedModules := []string{}
		for _, p := range changedP {
			changedModules = append(changedModules, p.ImportPath)
		}

		// the entrypoint files/folders need to be converted into directories, and then
		// from directories, to packages
		dirEntrypoints := []string{}
		for _, e := range entrypoints {
			dirEntrypoints = append(dirEntrypoints, path.Join(mainPackage, filepath.Dir(e)))
		}
		// convert directory entrypoints to packages entrypoints
		d, err := goList(dirEntrypoints...)
		if err != nil {
			panic(err)
		}
		entrypointPackages := []*PackagePlusExtras{}
		entrypointPackagesToTest := []string{}
		entrypointTips := []bool{}
		for i, r := range d {
			if len(hash(changedModules, []string{r.ImportPath})) > 0 {
				entrypointPackagesToTest = append(entrypointPackagesToTest, r.ImportPath)
			}
			// so now we take our entrypoints and check if any of the dependencies have changed
			h := hash(changedModules, r.Deps)
			if len(h) == 0 {
				entrypointTips = append(entrypointTips, false)
			} else {
				fmt.Printf("entrypoint %s has changed! \n", entrypoints[i])
				entrypointPackagesToTest = append(entrypointPackagesToTest, r.ImportPath)
			}
			entrypointPackages = append(entrypointPackages, r)
		}
		if len(entrypointPackages) == 0 {
			fmt.Println("no updates :)")
			return
		}

		walkPackages(mainPackage, entrypointPackages, changedModules)
		// fmt.Println(GetCwd())
		// fmt.Println()
		// fmt.Println(GetImports("github.com/braincorp/roc_services/internal/services/devices"))
		if len(packagesToTest) == 0 && len(entrypointPackagesToTest) == 0 {
			fmt.Println("no updates :) ")
			return
		}
		goTest(append(entrypointPackagesToTest, packagesToTest...)...)
	},
}

func main() {
	if !isGitAvailable() {
		fmt.Println("git: must be installed and accessable via $PATH")
		return
	}
	if !isGoAvailable() {
		fmt.Println("go: must be installed and accessable via $PATH")
		return
	}
	cmd.Execute()
}
