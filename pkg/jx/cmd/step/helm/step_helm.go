package helm

import (
	"fmt"
	"path/filepath"

	"github.com/jenkins-x/jx/pkg/helm"
	"github.com/jenkins-x/jx/pkg/jx/cmd/helper"
	"github.com/jenkins-x/jx/pkg/jx/cmd/step/git"
	"github.com/spf13/cobra"

	"os"

	"github.com/jenkins-x/jx/pkg/jx/cmd/opts"
	"github.com/jenkins-x/jx/pkg/log"
	"github.com/jenkins-x/jx/pkg/util"
)

const (
	PROW_JOB_ID   = "PROW_JOB_ID"
	REPO_OWNER    = "REPO_OWNER"
	REPO_NAME     = "REPO_NAME"
	PULL_PULL_SHA = "PULL_PULL_SHA"
)

// StepHelmOptions contains the command line flags
type StepHelmOptions struct {
	opts.StepOptions

	Dir         string
	https       bool
	GitProvider string
}

// NewCmdStepHelm Steps a command object for the "step" command
func NewCmdStepHelm(commonOpts *opts.CommonOptions) *cobra.Command {
	options := &StepHelmOptions{
		StepOptions: opts.StepOptions{
			CommonOptions: commonOpts,
		},
	}

	cmd := &cobra.Command{
		Use:   "helm",
		Short: "helm [command]",
		Run: func(cmd *cobra.Command, args []string) {
			options.Cmd = cmd
			options.Args = args
			err := options.Run()
			helper.CheckErr(err)
		},
	}
	cmd.AddCommand(NewCmdStepHelmApply(commonOpts))
	cmd.AddCommand(NewCmdStepHelmBuild(commonOpts))
	cmd.AddCommand(NewCmdStepHelmDelete(commonOpts))
	cmd.AddCommand(NewCmdStepHelmEnv(commonOpts))
	cmd.AddCommand(NewCmdStepHelmInstall(commonOpts))
	cmd.AddCommand(NewCmdStepHelmList(commonOpts))
	cmd.AddCommand(NewCmdStepHelmRelease(commonOpts))
	cmd.AddCommand(NewCmdStepHelmVersion(commonOpts))
	return cmd
}

// Run implements this command
func (o *StepHelmOptions) Run() error {
	return o.Cmd.Help()
}

func (o *StepHelmOptions) addStepHelmFlags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&o.Dir, "dir", "d", ".", "The directory containing the helm chart to apply")
	cmd.Flags().BoolVarP(&o.https, "clone-https", "", true, "Clone the environment Git repo over https rather than ssh which uses `git@foo/bar.git`")
	cmd.Flags().BoolVarP(&o.RemoteCluster, "remote", "", false, "If enabled assume we are in a remote cluster such as a stand alone Staging/Production cluster")
	cmd.Flags().StringVarP(&o.GitProvider, "git-provider", "", "github.com", "The Git provider for the environment Git repository")
}

func (o *StepHelmOptions) dropRepositories(repoIds []string, message string) error {
	var answer error
	for _, repoId := range repoIds {
		err := o.dropRepository(repoId, message)
		if err != nil {
			log.Logger().Warnf("Failed to drop repository %s: %s", util.ColorInfo(repoIds), util.ColorError(err))
			if answer == nil {
				answer = err
			}
		}
	}
	return answer
}

func (o *StepHelmOptions) dropRepository(repoId string, message string) error {
	if repoId == "" {
		return nil
	}
	log.Logger().Infof("Dropping helm release repository %s", util.ColorInfo(repoId))
	err := o.RunCommand("mvn",
		"org.sonatype.plugins:helm-staging-maven-plugin:1.6.5:rc-drop",
		"-DserverId=oss-sonatype-staging",
		"-DhelmUrl=https://oss.sonatype.org",
		"-DstagingRepositoryId="+repoId,
		"-Ddescription=\""+message+"\" -DstagingProgressTimeoutMinutes=60")
	if err != nil {
		log.Logger().Warnf("Failed to drop repository %s due to: %s", repoId, err)
	} else {
		log.Logger().Infof("Dropped repository %s", util.ColorInfo(repoId))
	}
	return err
}

func (o *StepHelmOptions) releaseRepository(repoId string) error {
	if repoId == "" {
		return nil
	}
	log.Logger().Infof("Releasing helm release repository %s", util.ColorInfo(repoId))
	options := o
	err := options.RunCommand("mvn",
		"org.sonatype.plugins:helm-staging-maven-plugin:1.6.5:rc-release",
		"-DserverId=oss-sonatype-staging",
		"-DhelmUrl=https://oss.sonatype.org",
		"-DstagingRepositoryId="+repoId,
		"-Ddescription=\"Next release is ready\" -DstagingProgressTimeoutMinutes=60")
	if err != nil {
		log.Logger().Infof("Failed to release repository %s due to: %s", repoId, err)
	} else {
		log.Logger().Infof("Released repository %s", util.ColorInfo(repoId))
	}
	return err
}

func (o *StepHelmOptions) cloneProwPullRequest(dir, gitProvider string) (string, error) {

	stepOpts := opts.StepOptions{
		CommonOptions: o.CommonOptions,
	}
	gitOpts := git.StepGitCredentialsOptions{
		StepOptions: stepOpts,
	}

	err := gitOpts.Run()
	if err != nil {
		return "", fmt.Errorf("failed to create Git credentials file: %v", err)
	}

	org := os.Getenv(REPO_OWNER)
	if org == "" {
		return "", fmt.Errorf("no %s env var found", REPO_OWNER)
	}

	repo := os.Getenv(REPO_NAME)
	if org == "" {
		return "", fmt.Errorf("no %s env var found", REPO_NAME)
	}

	var gitURL string
	if o.https {
		gitURL = fmt.Sprintf("https://%s/%s/%s.git", gitProvider, org, repo)
	} else {
		gitURL = fmt.Sprintf("git@%s:%s/%s.git", gitProvider, org, repo)
	}

	err = o.RunCommand("git", "clone", gitURL)
	if err != nil {
		return "", err
	}

	prCommit := os.Getenv(PULL_PULL_SHA)
	if org == "" {
		return "", fmt.Errorf("no %s env var found", PULL_PULL_SHA)
	}

	if dir != "" {
		dir = filepath.Join(dir, repo)
	} else {
		pwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(pwd, repo)
	}

	err = o.RunCommandFromDir(dir, "git", "checkout", prCommit)
	if err != nil {
		return "", err
	}

	err = o.RunCommand("cd", repo)
	if err != nil {
		return "", err
	}

	err = o.RunCommandFromDir(dir, "git", "checkout", "-b", "pr")
	if err != nil {
		return "", err
	}
	exists, err := util.FileExists(filepath.Join(dir, "env"))
	if err != nil {
		return "", err
	}
	if exists {
		dir = filepath.Join(dir, "env")
	}
	return dir, nil
}

func (o *StepHelmOptions) discoverValuesFiles(dir string) ([]string, error) {
	valuesFiles := []string{}
	for _, name := range []string{"values.yaml", helm.SecretsFileName, "myvalues.yaml"} {
		path := filepath.Join(dir, name)
		exists, err := util.FileExists(path)
		if err != nil {
			return valuesFiles, err
		}
		if exists {
			valuesFiles = append(valuesFiles, path)
		}
	}
	return valuesFiles, nil
}
