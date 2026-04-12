package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"dagger.io/dagger"
)

const (
	registryBase     = "ghcr.io/jkmlnop/bingo-cli"
	flyAppProduction = "bingo-server"
	flyAppStaging    = "bingo-server-staging"
	defaultGoVersion = "1.25.3"
)

// projectRoot returns the Go project root directory regardless of whether the
// pipeline is invoked from the repository root or the dagger/ subdirectory.
// When running via `cd dagger && go run . <cmd>` (e.g. from Lefthook), the CWD
// is the dagger/ subdirectory.  In that case the parent directory is the project
// root (identified by the presence of a go.mod file one level up).
func projectRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("failed to determine working directory: %v", err)
	}
	// If the current dir has go.mod AND the parent also has go.mod, we are inside
	// a nested module (dagger/) — step up to the project root.
	if _, err := os.Stat("go.mod"); err == nil {
		if _, err := os.Stat(filepath.Join("..", "go.mod")); err == nil {
			return filepath.Join(cwd, "..")
		}
	}
	return cwd
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	os.Args = append(os.Args[:1], os.Args[2:]...)

	version := flag.String("version", "dev", "version tag (git tag or short SHA)")
	env := flag.String("env", "", "deployment environment: staging or production")
	registryUser := flag.String("registry-user", "", "ghcr.io username")
	flag.Parse()

	ctx := context.Background()
	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		log.Fatalf("dagger connect: %v", err)
	}
	defer client.Close()

	source := client.Host().Directory(projectRoot(), dagger.HostDirectoryOpts{
		Exclude: []string{".git", "dagger", "bin", "bingo", "binGO-CLI", "binGO-CLI-*"},
	})

	switch cmd {
	case "test":
		err = runTest(ctx, client, source)
	case "test-container":
		err = runTestContainer(ctx, client, source)
	case "build":
		err = runBuild(ctx, client, source, *version)
	case "publish":
		err = runPublish(ctx, client, source, *version, *registryUser)
	case "deploy":
		err = runDeploy(ctx, client, source, *env, *version)
	case "release":
		err = runRelease(ctx, client, source, *version)
	case "all":
		err = runAll(ctx, client, source, *env, *version, *registryUser)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		log.Fatalf("%s failed: %v", cmd, err)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: go run ./dagger <command> [flags]

Commands:
  test             Run unit + integration tests (~30s)
  test-container   Run container regression suite (~10min, needs Docker)
  build            Build Docker image
  publish          Push image to ghcr.io
  deploy           Deploy to Fly.io (--env=staging|production)
  release          Cross-compile + create GitHub Release
  all              Full pipeline: test -> build -> publish -> deploy

Flags:
  --version        Version tag (default: dev)
  --env            Deployment environment: staging or production
  --registry-user  ghcr.io username (for publish/all)`)
}

func runTest(ctx context.Context, client *dagger.Client, source *dagger.Directory) error {
	goContainer := goBase(client, source)
	fmt.Println("=== Running unit tests ===")
	_, err := goContainer.WithExec([]string{"go", "test", "./..."}).Sync(ctx)
	if err != nil {
		return fmt.Errorf("unit tests: %w", err)
	}
	fmt.Println("=== Running integration tests ===")
	_, err = goContainer.WithExec([]string{"go", "test", "-tags=integration", "./tests", "-v"}).Sync(ctx)
	if err != nil {
		return fmt.Errorf("integration tests: %w", err)
	}
	fmt.Println("=== All tests passed ===")
	return nil
}

func runTestContainer(ctx context.Context, client *dagger.Client, source *dagger.Directory) error {
	fmt.Println("=== Running container regression tests ===")
	dockerSocket := client.Host().UnixSocket("/var/run/docker.sock")
	_, err := goBase(client, source).
		WithUnixSocket("/var/run/docker.sock", dockerSocket).
		// When tests run inside a Dagger container with the host Docker socket
		// mounted, sibling containers are created on the host network.
		// - RYUK disabled: Ryuk's Reaper binds a port on the host; inside the
		//   Dagger container "localhost" can't reach that port. Tests already
		//   call container.Terminate() explicitly, so Ryuk is not needed.
		// - HOST_OVERRIDE: c.Host() returns this value so mapped ports are
		//   looked up on the Docker Desktop hostname instead of localhost.
		//   Works on macOS/Windows (Docker Desktop). On Linux CI the container
		//   tests are not run inside Dagger (Lefthook runs them directly).
		WithEnvVariable("TESTCONTAINERS_RYUK_DISABLED", "true").
		WithEnvVariable("TESTCONTAINERS_HOST_OVERRIDE", "host.docker.internal").
		WithExec([]string{"go", "test", "-tags=container", "-timeout=10m", "./tests", "-v"}).
		Sync(ctx)
	if err != nil {
		return fmt.Errorf("container tests: %w", err)
	}
	fmt.Println("=== Container tests passed ===")
	return nil
}

func runBuild(ctx context.Context, client *dagger.Client, source *dagger.Directory, version string) error {
	fmt.Printf("=== Building image (version=%s) ===\n", version)
	_, err := buildImage(client, source, version).Sync(ctx)
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	fmt.Println("=== Build complete ===")
	return nil
}

func runPublish(ctx context.Context, client *dagger.Client, source *dagger.Directory, version, registryUser string) error {
	if registryUser == "" {
		return fmt.Errorf("--registry-user is required for publish")
	}
	token := os.Getenv("GHCR_TOKEN")
	if token == "" {
		return fmt.Errorf("GHCR_TOKEN environment variable is required")
	}
	secret := client.SetSecret("ghcr-token", token)
	container := buildImage(client, source, version)
	versionTag := fmt.Sprintf("%s:%s", registryBase, version)
	latestTag := fmt.Sprintf("%s:latest", registryBase)
	fmt.Printf("=== Publishing %s ===\n", versionTag)
	addr, err := container.WithRegistryAuth("ghcr.io", registryUser, secret).Publish(ctx, versionTag)
	if err != nil {
		return fmt.Errorf("publish %s: %w", versionTag, err)
	}
	fmt.Printf("Published: %s\n", addr)
	_, err = container.WithRegistryAuth("ghcr.io", registryUser, secret).Publish(ctx, latestTag)
	if err != nil {
		return fmt.Errorf("publish %s: %w", latestTag, err)
	}
	fmt.Printf("Published: %s\n", latestTag)
	return nil
}

func runDeploy(ctx context.Context, client *dagger.Client, source *dagger.Directory, env, version string) error {
	appName, configFile, err := flyConfig(env)
	if err != nil {
		return err
	}
	flyToken := os.Getenv("FLY_API_TOKEN")
	if flyToken == "" {
		return fmt.Errorf("FLY_API_TOKEN environment variable is required")
	}
	secret := client.SetSecret("fly-token", flyToken)
	imageRef := fmt.Sprintf("%s:%s", registryBase, version)
	fmt.Printf("=== Deploying %s to %s (%s) ===\n", imageRef, env, appName)
	_, err = client.Container().From("flyio/flyctl:latest").
		WithSecretVariable("FLY_API_TOKEN", secret).
		WithMountedDirectory("/app", source).
		WithWorkdir("/app").
		WithExec([]string{"flyctl", "deploy", "--app", appName, "--config", configFile, "--image", imageRef, "--yes", "--wait-timeout=300"}).
		Sync(ctx)
	if err != nil {
		return fmt.Errorf("deploy to %s (%s): %w", env, appName, err)
	}
	fmt.Printf("=== Deployed to %s ===\n", env)
	return nil
}

func runRelease(ctx context.Context, client *dagger.Client, source *dagger.Directory, version string) error {
	ghToken := os.Getenv("GH_TOKEN")
	if ghToken == "" {
		return fmt.Errorf("GH_TOKEN environment variable is required")
	}
	secret := client.SetSecret("gh-token", ghToken)
	goContainer := client.Container().
		From(fmt.Sprintf("golang:%s-alpine", defaultGoVersion)).
		WithMountedDirectory("/src", source).
		WithWorkdir("/src").
		WithEnvVariable("CGO_ENABLED", "0")
	fmt.Println("=== Building release binaries ===")
	macBinary := goContainer.
		WithEnvVariable("GOOS", "darwin").
		WithEnvVariable("GOARCH", "amd64").
		WithExec([]string{"go", "build", "-ldflags", fmt.Sprintf("-X main.version=%s", version), "-o", "/out/binGO-CLI-intel-mac", "."}).
		File("/out/binGO-CLI-intel-mac")
	linuxBinary := goContainer.
		WithEnvVariable("GOOS", "linux").
		WithEnvVariable("GOARCH", "amd64").
		WithExec([]string{"go", "build", "-ldflags", fmt.Sprintf("-X main.version=%s", version), "-o", "/out/binGO-CLI-linux", "."}).
		File("/out/binGO-CLI-linux")
	fmt.Printf("=== Creating GitHub Release %s ===\n", version)
	_, err := client.Container().
		From("alpine:3.19").
		WithExec([]string{"apk", "add", "--no-cache", "coreutils", "curl", "tar"}).
		WithExec([]string{"sh", "-c", "curl -sL https://github.com/cli/cli/releases/download/v2.63.2/gh_2.63.2_linux_amd64.tar.gz | tar xz -C /usr/local --strip-components=1"}).
		WithSecretVariable("GH_TOKEN", secret).
		WithFile("/release/binGO-CLI-intel-mac", macBinary).
		WithFile("/release/binGO-CLI-linux", linuxBinary).
		WithWorkdir("/release").
		WithExec([]string{"sh", "-c", "sha256sum binGO-CLI-intel-mac binGO-CLI-linux > SHA256SUMS"}).
		WithExec([]string{"gh", "release", "create", version, "--repo", "jkMLnop/binGO-CLI", "--title", version, "--generate-notes", "binGO-CLI-intel-mac", "binGO-CLI-linux", "SHA256SUMS"}).
		Sync(ctx)
	if err != nil {
		return fmt.Errorf("release %s: %w", version, err)
	}
	fmt.Printf("=== Released %s ===\n", version)
	return nil
}

func runAll(ctx context.Context, client *dagger.Client, source *dagger.Directory, env, version, registryUser string) error {
	if err := runTest(ctx, client, source); err != nil {
		return err
	}
	if err := runPublish(ctx, client, source, version, registryUser); err != nil {
		return err
	}
	if err := runDeploy(ctx, client, source, env, version); err != nil {
		return err
	}
	return nil
}

func goBase(client *dagger.Client, source *dagger.Directory) *dagger.Container {
	return client.Container().
		From(fmt.Sprintf("golang:%s-alpine", defaultGoVersion)).
		WithExec([]string{"apk", "add", "--no-cache", "gcc", "musl-dev", "sqlite-dev"}).
		WithMountedDirectory("/src", source).
		WithWorkdir("/src").
		WithEnvVariable("CGO_ENABLED", "1")
}

func buildImage(client *dagger.Client, source *dagger.Directory, version string) *dagger.Container {
	return client.Container().
		Build(source, dagger.ContainerBuildOpts{
			Dockerfile: "Dockerfile",
			BuildArgs: []dagger.BuildArg{
				{Name: "VERSION", Value: version},
				{Name: "GO_VERSION", Value: defaultGoVersion},
			},
		})
}

func flyConfig(env string) (appName, configFile string, err error) {
	switch env {
	case "production":
		return flyAppProduction, "fly.toml", nil
	case "staging":
		return flyAppStaging, "fly.staging.toml", nil
	default:
		return "", "", fmt.Errorf("invalid environment %q: must be \"staging\" or \"production\"", env)
	}
}
