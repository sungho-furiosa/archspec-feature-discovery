/*
Copyright 2020 Red Hat

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/docopt/docopt-go"
)

const (
	// Bin : Name of the binary
	Bin = "spack-feature-discovery"
)

var (
	// Version : Version of the binary
	// This will be set using ldflags at compile time
	Version = "0.1.0"
	// SpackV : Version of Spack
	SpackV = ""
)

// Conf represents SFD configuration options
type Conf struct {
	LabelOnce      bool
	OutputFilePath string
	SleepInterval  time.Duration
}

func main() {

	log.Printf("Running %s in version %s", Bin, Version)

	conf := Conf{}
	conf.getConfFromArgv(os.Args)
	conf.getConfFromEnv()
	log.Print("Loaded configuration:")
	log.Print("SleepInterval: ", conf.SleepInterval)
	log.Print("OutputFilePath: ", conf.OutputFilePath)

	log.Print("Start running")
	err := run(conf)
	if err != nil {
		log.Printf("Unexpected error: %v", err)
	}
	log.Print("Exiting")
}

func getMicroArch() (string, error) {

	spack, err := exec.LookPath("spack")
	if err != nil {
		return "", err
	}

	args := []string{"arch", "-t"}

	cmd := exec.Command(spack, args...)
	stdout, _ := cmd.Output()
	arch := string(stdout)

	return strings.TrimSpace(arch), nil
}

func (conf *Conf) getConfFromArgv(argv []string) {
	usage := fmt.Sprintf(`%[1]s:
Usage:
  %[1]s [--oneshot | --sleep-interval=<seconds>] [--output-file=<file> | -o <file>]
  %[1]s -h | --help
  %[1]s --version

Options:
  -h --help                       Show this help message and exit
  --version                       Display version and exit
  --sleep-interval=<seconds>      Time to sleep between labeling [Default: 60s]
  -o <file> --output-file=<file>  Path to output file
                                  [Default: /etc/kubernetes/node-feature-discovery/features.d/sfd]`,
		Bin)

	opts, err := docopt.ParseArgs(usage, argv[1:], Bin+" "+Version)
	if err != nil {
		log.Fatal("Error while parsing command line options: ", err)
	}
	conf.LabelOnce, err = opts.Bool("--labelonce")
	if err != nil {
		log.Fatal("Error while parsing command line options: ", err)
	}
	sleepIntervalString, err := opts.String("--sleep-interval")
	if err != nil {
		log.Fatal("Error while parsing command line options: ", err)
	}
	conf.OutputFilePath, err = opts.String("--output-file")
	if err != nil {
		log.Fatal("Error while parsing command line options: ", err)
	}

	conf.SleepInterval, err = time.ParseDuration(sleepIntervalString)
	if err != nil {
		log.Fatal("Invalid value for --sleep-interval option: ", err)
	}

	return
}

func (conf *Conf) getConfFromEnv() {

	val, ok := os.LookupEnv("SFD_LABELONCE")
	if ok && strings.EqualFold(val, "true") {
		conf.LabelOnce = true
	}

	sleepIntervalString, ok := os.LookupEnv("SFD_SLEEP_INTERVAL")
	if ok {
		var err error
		conf.SleepInterval, err = time.ParseDuration(sleepIntervalString)
		if err != nil {
			log.Fatal("Invalid value from env for sleep-interval option: ", err)
		}
	}
	outputFilePathTmp, ok := os.LookupEnv("SFD_OUTPUT_FILE")
	if ok {
		conf.OutputFilePath = outputFilePathTmp
	}
}

func run(conf Conf) error {

	outputFileAbsPath, err := filepath.Abs(conf.OutputFilePath)
	if err != nil {
		return fmt.Errorf("Failed to retrieve absolute path of output file: %v", err)
	}
	tmpDirPath := filepath.Dir(outputFileAbsPath) + "/sfd-tmp"

	err = os.Mkdir(tmpDirPath, os.ModePerm)
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("Failed to create temporary directory: %v", err)
	}

L:
	for {
		tmpOutputFile, err := ioutil.TempFile(tmpDirPath, "sfd-")
		if err != nil {
			return fmt.Errorf("Fail to create temporary output file: %v", err)
		}

		arch, err := getMicroArch()
		if err != nil {
			return fmt.Errorf("Fail to retrieve MicroArch info from Spack: %v", err)
		}

		log.Print("Writing labels to output file")
		fmt.Fprintf(tmpOutputFile, "spack.io/sfd.timestamp=%d\n", time.Now().Unix())
		fmt.Fprintf(tmpOutputFile, "spack.io/sfd.spack.version=%d\n", SpackV)
		fmt.Fprintf(tmpOutputFile, "spack.io/sfd.arch.target=%s\n", arch)

		err = tmpOutputFile.Chmod(0644)
		if err != nil {
			return fmt.Errorf("Error chmod temporary file: %v", err)
		}

		err = tmpOutputFile.Close()
		if err != nil {
			return fmt.Errorf("Error closing temporary file: %v", err)
		}

		err = os.Rename(tmpOutputFile.Name(), conf.OutputFilePath)
		if err != nil {
			return fmt.Errorf("Error moving temporary file '%s': %v", conf.OutputFilePath, err)
		}

		if conf.LabelOnce {
			break
		}

		log.Print("Sleeping for ", conf.SleepInterval)

		select {
		case <-exitChan:
			break L
		case <-time.After(conf.SleepInterval):
			break
		}
	}

	return nil
}
