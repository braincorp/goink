package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

type Module struct {
	Path      string
	Main      bool
	Dir       string
	GoMod     string
	GoVersion string
}

type PackagePlusExtras struct {
	build.Package
	Deps   []string
	Module Module
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
	t := []string{}
	for _, r := range strings.Split(strings.TrimSpace(string(data)), fmt.Sprintln()) {
		if r != "" {
			t = append(t, r)
		}
	}
	return t
}
func goTest(flags string, ss ...string) {
	fmt.Print("go ", "test ", flags)
	fmt.Println(strings.Join(ss, " "))
	cmd := exec.Command("go", append([]string{"test", flags}, ss...)...)
	cmd.Stdout = os.Stdout
	cmd.Dir = GetCwd()

	_ = cmd.Run()
	return
}
func goList(ss ...string) ([]*PackagePlusExtras, error) {
	r := append([]string{"list", "-f", "{}", "-json"}, ss...)
	//fmt.Println("go", r)
	cmd := exec.Command("go", r...)
	cmd.Dir = GetCwd()
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
func GetGitChangedFiles(branch string) []string {
	r := goGit("diff", goGit("merge-base", "--fork-point", branch)[0], "--name-only")

	if len(r) != 0 {
		fmt.Println("changed files:")
		fmt.Println(strings.Join(r, ","))
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
	cmd.Flags().String("go-test-flags", "", "flags to pass when invoking go test. ")

}

var lock = sync.Mutex{}
var packagesToTest = []string{}
var seenPackages = map[string]bool{}

func walkPackage(pack *PackagePlusExtras, changedPackages []string) {
	// if len(hash([]string{pack.ImportPath}, changedPackages)) == 0 {
	// 	packagesToTest = append(packagesToTest, pack.ImportPath)
	// }
	walkPackagesInner(pack.Module.Path, []*PackagePlusExtras{pack}, changedPackages)
	return
}

func walkPackagesInner(base string, packages []*PackagePlusExtras, changedPackages []string) {
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
	walkPackagesInner(base, t1, changedPackages)
}

var cmd = cobra.Command{
	Short: "test",
	Run: func(cmd *cobra.Command, args []string) {
		head, _ := cmd.Flags().GetString("head")
		testFlags, _ := cmd.Flags().GetString("go-test-flags")

		entrypointsArg, _ := cmd.Flags().GetStringSlice("entrypoints")
		// need to test all packages in this folder if they are dependent on the
		// changed files. Convert the changed files into their respective packages (folders)
		gitFiles := GetGitChangedFiles(head)
		if len(gitFiles) == 0 {
			fmt.Println("no files changed :)")
			return
		}
		changedDirectories := map[string]bool{}
		for _, f := range gitFiles {
			changedDirectories[filepath.Dir(f)] = true
		}
		for _, entrypoint := range entrypointsArg {

			// convert directory entrypoints to packages entrypoints
			d, err := goList(entrypoint)
			if err != nil {
				panic(err)
			}
			entrypointPackage := d[0]

			changedDirVar := []string{}
			for f := range changedDirectories {
				changedDirVar = append(changedDirVar, filepath.Join(entrypointPackage.Module.Path, f))
			}
			changedP, err := goList(changedDirVar...)
			if err != nil {
				panic(err)
			}

			changedModules := []string{}
			for _, p := range changedP {
				changedModules = append(changedModules, p.ImportPath)
			}
			fmt.Println("changed Modules", changedModules)

			h := hash(changedModules, entrypointPackage.Deps)
			if len(h) != 0 {
				fmt.Printf("entrypoint %s has changed! \n", entrypoint)
			}
			packagesToTest = append(packagesToTest, changedModules...)

			// so now we take our entrypoints and check if any of the dependencies have changed
			walkPackage(entrypointPackage, changedModules)
		}
		// fmt.Println(GetCwd())
		// fmt.Println()
		// fmt.Println(GetImports("github.com/braincorp/roc_services/internal/services/devices"))
		goTest(testFlags, packagesToTest...)
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
