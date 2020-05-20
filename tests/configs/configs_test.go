package configs_test

import (
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gexec"
	"gopkg.in/yaml.v1"
)

var _ = Describe("Configs", func() {
	var (
		args          []string
		baseHash      [16]byte
		repoDir       string
		templatedPath string = "/tmp/cf-for-k8s.yml"
	)
	BeforeEach(func() {
		currentDirectory, err := os.Getwd()
		repoDir = filepath.Dir(filepath.Dir(currentDirectory))

		command := exec.Command(filepath.Join(repoDir, "hack", "generate-values.sh"),
			"--cf-domain", "dummy-domain",
		)
		valuesFile, err := os.Create("/tmp/dummy-domain-values.yml")
		Expect(err).NotTo(HaveOccurred())
		defer valuesFile.Close()
		command.Stdout = valuesFile

		err = command.Start()
		Expect(err).NotTo(HaveOccurred())

		print("generating fake values...")
		command.Wait()
		print(" [done]\n")

		args = []string{
			"-f", "../../config",
			"-f", "/tmp/dummy-domain-values.yml",
		}
		outfile, err := os.Create(templatedPath)
		Expect(err).NotTo(HaveOccurred())
		defer outfile.Close()
		command = exec.Command("ytt", args...)
		session, err := Start(command, outfile, GinkgoWriter)
		Expect(err).NotTo(HaveOccurred())
		Eventually(session, 10*time.Second).Should(Exit(0),
			fmt.Sprintf("ytt failed on base with output %s", session.Err.Contents()))
		baseHash = md5.Sum(session.Out.Contents())
	})
	Describe("Check optional configs", func() {

		It("should load each optional config file", func() {
			currentDirectory, err := os.Getwd()
			Expect(err).ToNot(HaveOccurred())

			configDirectory := filepath.Join(filepath.Dir(filepath.Dir(currentDirectory)), "config-optional")

			count := 0
			filepath.Walk(configDirectory, func(path string, info os.FileInfo, err error) error {

				basename := info.Name()
				if !strings.HasSuffix(basename, ".yml") {
					return nil
				}
				fmt.Printf("Test file %s\n", basename)
				finalArgs := append(args, "-f", path)
				command := exec.Command("ytt", finalArgs...)

				session, err := Start(command, ioutil.Discard, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())
				session.Wait(10 * time.Second)
				Eventually(session).Should(Exit(0),
					fmt.Sprintf("ytt failed on %s with output %s", path, session.Err.Contents()))
				newHash := md5.Sum(session.Out.Contents())
				Expect(newHash).NotTo(Equal(baseHash), fmt.Sprintf("optional file %s had no effect", basename))
				count += 1

				return nil
			})
			Expect(count).To(BeNumerically(">=", 1))
		})
	})
	When("validating with kubeval", func() {
		It("should pass", func() {
			for _, v := range getSupportedK8Versions() {
				By(fmt.Sprintf("checking with K8s version '%s'", v))
				command := exec.Command("kubeval", "--strict", "--ignore-missing-schemas", "-v", v, templatedPath)
				fmt.Println(command.Args)
				session, err := Start(command, ioutil.Discard, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())
				session.Wait(10 * time.Second)
				stdOut := removeExpectedKubevalOutput(session.Out.Contents())
				Eventually(session).Should(Exit(0),
					fmt.Sprintf("kubeval failed with (filtered) output: %s\n", stdOut))
			}
		})
	})
})

type supportedK8sVersions struct {
	OldestVersion string `yaml:"oldest_version"`
	NewestVersion string `yaml:"newest_version"`
}

func getSupportedK8Versions() []string {
	currentDirectory, err := os.Getwd()
	repoDir := filepath.Dir(filepath.Dir(currentDirectory))

	v := supportedK8sVersions{}
	f, err := ioutil.ReadFile(filepath.Join(repoDir, "supported_k8s_versions.yml"))
	Expect(err).NotTo(HaveOccurred())
	err = yaml.Unmarshal(f, &v)
	Expect(err).NotTo(HaveOccurred())
	Expect(v.NewestVersion).ToNot(Equal(""))
	return []string{v.NewestVersion, v.OldestVersion}
}

func removeExpectedKubevalOutput(output []byte) []byte {
	reMissingSchema := regexp.MustCompile("(?m)[\r\n]+^.*not validated against a schema$")
	rePassed := regexp.MustCompile("(?m)[\r\n]+^PASS - .*$")
	res := reMissingSchema.ReplaceAll(output, []byte(""))
	return rePassed.ReplaceAll(res, []byte(""))
}
