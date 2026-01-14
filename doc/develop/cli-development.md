# Skupper CLI Development Guide

This guide covers the architecture, patterns, and best practices for developing Skupper CLI commands using the Cobra framework.

## Table of Contents

- [Overview](#overview)
- [CLI Architecture](#cli-architecture)
- [Command Structure](#command-structure)
- [Creating a New Command](#creating-a-new-command)
- [Platform Abstraction](#platform-abstraction)
- [Validation Patterns](#validation-patterns)
- [Error Handling](#error-handling)
- [Flags and Configuration](#flags-and-configuration)
- [Testing](#testing)
- [Code Style Guidelines](#code-style-guidelines)
- [Common Patterns](#common-patterns)

## Overview

The Skupper CLI is built using [Cobra](https://github.com/spf13/cobra), a popular Go library for creating powerful CLI applications. The CLI supports multiple platforms (Kubernetes, Podman, Docker, Linux) with a unified interface.

**Key Design Principles:**
- Platform abstraction (Kubernetes vs. non-Kubernetes)
- Consistent command structure across all commands
- Comprehensive input validation
- Clear error messages with actionable guidance
- Separation of concerns (validation, execution, waiting)

## CLI Architecture

### Directory Structure

```
cmd/skupper/
  main.go                          # Entry point
internal/cmd/skupper/
  root/
    root.go                        # Root command definition
  common/
    command.go                     # Command interfaces and factory
    flags.go                       # Flag definitions and structs
    constants.go                   # Platform constants and types
    utils/
      handle_error.go              # Error handling utilities
      spinner.go                   # Progress indicators
      status.go                    # Status checking utilities
  token/
    token.go                       # Token command group
    kube/
      token_issue.go               # Kubernetes implementation
      token_redeem.go
    nonkube/
      token_issue.go               # Non-Kubernetes implementation
      token_redeem.go
  version/
    version.go                     # Version command
    kube/version.go
    nonkube/version.go
  [other commands...]
```

### Component Layers

```
┌─────────────────────────────────────────────────────────┐
│ main.go                                                 │
│ - Entry point                                           │
│ - Calls root command Execute()                         │
└────────────────────┬────────────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────────────┐
│ Root Command (root/root.go)                            │
│ - Registers all subcommands                            │
│ - Defines global flags (--platform, --namespace, etc.) │
│ - Sets up help command                                 │
└────────────────────┬────────────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────────────┐
│ Command Groups (token/, version/, site/, etc.)         │
│ - Organize related commands                            │
│ - Create command hierarchy                             │
└────────────────────┬────────────────────────────────────┘
                     │
┌────────────────────▼────────────────────────────────────┐
│ Platform Factory (common/command.go)                   │
│ - ConfigureCobraCommand()                              │
│ - Routes to Kubernetes or non-Kubernetes impl          │
└────────────────────┬────────────────────────────────────┘
                     │
        ┌────────────┴────────────┐
        │                         │
┌───────▼──────────┐    ┌────────▼─────────┐
│ Kubernetes Impl  │    │ Non-Kube Impl    │
│ (kube/)          │    │ (nonkube/)       │
│ - K8s client     │    │ - Container/FS   │
│ - CRD operations │    │ - Local config   │
└──────────────────┘    └──────────────────┘
```

## Command Structure

### The SkupperCommand Interface

All command implementations must satisfy the `SkupperCommand` interface:

```go
// internal/cmd/skupper/common/command.go
type SkupperCommand interface {
    NewClient(cobraCommand *cobra.Command, args []string)
    ValidateInput(args []string) error
    InputToOptions()
    Run() error
    WaitUntil() error
}
```

**Method Responsibilities:**

1. **NewClient**: Initialize platform-specific client (K8s client, container runtime, etc.)
2. **ValidateInput**: Validate all inputs (args, flags) before execution
3. **InputToOptions**: Transform validated inputs into internal options/config
4. **Run**: Execute the main command logic
5. **WaitUntil**: Wait for async operations to complete (optional)

### Command Lifecycle

```
User runs command
       │
       ▼
┌──────────────────┐
│ PreRunE          │  1. Determine platform (kubernetes/podman/docker/linux)
│                  │  2. Select implementation (kube vs nonkube)
│                  │  3. Call NewClient()
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Run              │  4. Call ValidateInput() - exit on error
│                  │  5. Call InputToOptions()
│                  │  6. Call Run() - exit on error
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ PostRun          │  7. Call WaitUntil() - exit on error
└──────────────────┘
```

## Creating a New Command

### Step 1: Define Command Structure

Create a new directory under `internal/cmd/skupper/`:

```bash
mkdir -p internal/cmd/skupper/mycommand/{kube,nonkube}
```

### Step 2: Create Command Group File

```go
// internal/cmd/skupper/mycommand/mycommand.go
package mycommand

import (
    "github.com/skupperproject/skupper/internal/cmd/skupper/common"
    "github.com/skupperproject/skupper/internal/cmd/skupper/mycommand/kube"
    "github.com/skupperproject/skupper/internal/cmd/skupper/mycommand/nonkube"
    "github.com/skupperproject/skupper/internal/config"
    "github.com/spf13/cobra"
)

func NewCmdMyCommand() *cobra.Command {
    platform := common.Platform(config.GetPlatform())
    cmd := CmdMyCommandFactory(platform)
    return cmd
}

func CmdMyCommandFactory(configuredPlatform common.Platform) *cobra.Command {
    kubeCommand := kube.NewCmdMyCommand()
    nonKubeCommand := nonkube.NewCmdMyCommand()

    cmdDesc := common.SkupperCmdDescription{
        Use:     "mycommand <arg>",
        Short:   "Brief description of mycommand",
        Long:    "Detailed description of what mycommand does.",
        Example: "skupper mycommand example-arg --flag=value",
    }

    cmd := common.ConfigureCobraCommand(
        configuredPlatform,
        cmdDesc,
        kubeCommand,
        nonKubeCommand,
    )

    // Define flags
    cmdFlags := common.CommandMyCommandFlags{}
    cmd.Flags().StringVar(&cmdFlags.SomeFlag, "some-flag", "", "Description")
    cmd.Flags().DurationVar(&cmdFlags.Timeout, common.FlagNameTimeout, 60*time.Second, common.FlagDescTimeout)

    // Attach flags to both implementations
    kubeCommand.CobraCmd = cmd
    kubeCommand.Flags = &cmdFlags
    nonKubeCommand.CobraCmd = cmd
    nonKubeCommand.Flags = &cmdFlags

    return cmd
}
```

### Step 3: Implement Kubernetes Version

```go
// internal/cmd/skupper/mycommand/kube/mycommand.go
package kube

import (
    "context"
    "errors"
    "fmt"

    "github.com/skupperproject/skupper/internal/cmd/skupper/common"
    "github.com/skupperproject/skupper/internal/cmd/skupper/common/utils"
    "github.com/skupperproject/skupper/internal/kube/client"
    skupperv2alpha1 "github.com/skupperproject/skupper/pkg/generated/client/clientset/versioned/typed/skupper/v2alpha1"
    "github.com/spf13/cobra"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type CmdMyCommand struct {
    client    skupperv2alpha1.SkupperV2alpha1Interface
    CobraCmd  *cobra.Command
    Flags     *common.CommandMyCommandFlags
    namespace string
    // Internal state
    argValue  string
}

func NewCmdMyCommand() *CmdMyCommand {
    return &CmdMyCommand{}
}

// NewClient initializes the Kubernetes client
func (cmd *CmdMyCommand) NewClient(cobraCommand *cobra.Command, args []string) {
    cli, err := client.NewClient(
        cobraCommand.Flag("namespace").Value.String(),
        cobraCommand.Flag("context").Value.String(),
        cobraCommand.Flag("kubeconfig").Value.String(),
    )
    utils.HandleError(utils.GenericError, err)

    cmd.client = cli.GetSkupperClient().SkupperV2alpha1()
    cmd.namespace = cli.Namespace
}

// ValidateInput validates all inputs before execution
func (cmd *CmdMyCommand) ValidateInput(args []string) error {
    var validationErrors []error

    // Validate CRDs are installed
    _, err := cmd.client.Sites(cmd.namespace).List(context.TODO(), metav1.ListOptions{})
    if err != nil {
        validationErrors = append(validationErrors, utils.HandleMissingCrds(err))
        return errors.Join(validationErrors...)
    }

    // Validate arguments
    if len(args) < 1 {
        validationErrors = append(validationErrors, fmt.Errorf("argument required"))
    } else if len(args) > 1 {
        validationErrors = append(validationErrors, fmt.Errorf("only one argument allowed"))
    } else {
        cmd.argValue = args[0]
    }

    // Validate flags
    if cmd.Flags.SomeFlag == "" {
        validationErrors = append(validationErrors, fmt.Errorf("--some-flag is required"))
    }

    return errors.Join(validationErrors...)
}

// InputToOptions transforms inputs to internal options
func (cmd *CmdMyCommand) InputToOptions() {
    // Transform validated inputs if needed
    // This is where you'd convert string flags to typed values,
    // apply defaults, etc.
}

// Run executes the main command logic
func (cmd *CmdMyCommand) Run() error {
    // Create or update Kubernetes resources
    resource := v2alpha1.MyResource{
        TypeMeta: metav1.TypeMeta{
            APIVersion: "skupper.io/v2alpha1",
            Kind:       "MyResource",
        },
        ObjectMeta: metav1.ObjectMeta{
            Name: cmd.argValue,
        },
        Spec: v2alpha1.MyResourceSpec{
            // Set spec fields from flags
        },
    }

    _, err := cmd.client.MyResources(cmd.namespace).Create(
        context.TODO(),
        &resource,
        metav1.CreateOptions{},
    )
    return err
}

// WaitUntil waits for async operations to complete
func (cmd *CmdMyCommand) WaitUntil() error {
    if cmd.Flags.Timeout == 0 {
        return nil // No waiting requested
    }

    waitTime := int(cmd.Flags.Timeout.Seconds())
    return utils.NewSpinnerWithTimeout("Waiting for resource...", waitTime, func() error {
        resource, err := cmd.client.MyResources(cmd.namespace).Get(
            context.TODO(),
            cmd.argValue,
            metav1.GetOptions{},
        )
        if err != nil {
            return err
        }

        if resource.IsReady() {
            return nil // Success - stop waiting
        }

        return fmt.Errorf("not ready yet") // Keep waiting
    })
}
```

### Step 4: Implement Non-Kubernetes Version

```go
// internal/cmd/skupper/mycommand/nonkube/mycommand.go
package nonkube

import (
    "errors"
    "fmt"

    "github.com/skupperproject/skupper/internal/cmd/skupper/common"
    "github.com/skupperproject/skupper/internal/cmd/skupper/common/utils"
    "github.com/skupperproject/skupper/internal/nonkube/client"
    "github.com/spf13/cobra"
)

type CmdMyCommand struct {
    client    *client.SkupperClient
    CobraCmd  *cobra.Command
    Flags     *common.CommandMyCommandFlags
    namespace string
    argValue  string
}

func NewCmdMyCommand() *CmdMyCommand {
    return &CmdMyCommand{}
}

func (cmd *CmdMyCommand) NewClient(cobraCommand *cobra.Command, args []string) {
    cli, err := client.NewClient(
        cobraCommand.Flag("namespace").Value.String(),
        cobraCommand.Flag("platform").Value.String(),
    )
    utils.HandleError(utils.GenericError, err)

    cmd.client = cli
    cmd.namespace = cli.Namespace
}

func (cmd *CmdMyCommand) ValidateInput(args []string) error {
    var validationErrors []error

    // Validate site exists
    if !cmd.client.SiteExists() {
        validationErrors = append(validationErrors, 
            fmt.Errorf("no site found in namespace %s", cmd.namespace))
    }

    // Validate arguments
    if len(args) < 1 {
        validationErrors = append(validationErrors, fmt.Errorf("argument required"))
    } else {
        cmd.argValue = args[0]
    }

    return errors.Join(validationErrors...)
}

func (cmd *CmdMyCommand) InputToOptions() {
    // Transform inputs
}

func (cmd *CmdMyCommand) Run() error {
    // Implement non-Kubernetes logic
    // This might involve:
    // - Writing to filesystem
    // - Calling container runtime APIs
    // - Updating local configuration files
    return cmd.client.CreateMyResource(cmd.argValue, cmd.Flags)
}

func (cmd *CmdMyCommand) WaitUntil() error {
    // Implement waiting logic for non-Kubernetes
    return nil
}
```

### Step 5: Register Command

Add to root command:

```go
// internal/cmd/skupper/root/root.go
func NewSkupperRootCommand() *cobra.Command {
    rootCmd.AddCommand(site.NewCmdSite())
    rootCmd.AddCommand(token.NewCmdToken())
    rootCmd.AddCommand(mycommand.NewCmdMyCommand()) // Add here
    // ... other commands
    return rootCmd
}
```

### Step 6: Add Flag Definitions

```go
// internal/cmd/skupper/common/flags.go
const (
    FlagNameSomeFlag = "some-flag"
    FlagDescSomeFlag = "Description of what this flag does"
)

type CommandMyCommandFlags struct {
    SomeFlag string
    Timeout  time.Duration
}
```

## Platform Abstraction

### Platform Detection

Platform is determined in this order:

1. `--platform` flag (highest priority)
2. `SKUPPER_PLATFORM` environment variable
3. Auto-detection (checks for Kubernetes config)

```go
// internal/config/platform.go
func GetPlatform() string {
    // 1. Check flag (set by Cobra)
    if Platform != "" {
        return Platform
    }
    
    // 2. Check environment
    if platform := os.Getenv("SKUPPER_PLATFORM"); platform != "" {
        return platform
    }
    
    // 3. Auto-detect
    if _, err := clientcmd.NewDefaultClientConfigLoadingRules().Load(); err == nil {
        return "kubernetes"
    }
    
    return "podman" // Default
}
```

### Platform-Specific Code

Use the factory pattern to route to the correct implementation:

```go
// internal/cmd/skupper/common/command.go
func ConfigureCobraCommand(
    configuredPlatform Platform,
    description SkupperCmdDescription,
    kubeImpl SkupperCommand,
    nonKubeImpl SkupperCommand,
) *cobra.Command {
    var skupperCommand SkupperCommand

    cmd := cobra.Command{
        Use:     description.Use,
        Short:   description.Short,
        Long:    description.Long,
        Example: description.Example,
        PreRunE: func(cmd *cobra.Command, args []string) error {
            platform := string(configuredPlatform)
            if cmd.Flag("platform") != nil && cmd.Flag("platform").Value.String() != "" {
                platform = cmd.Flag("platform").Value.String()
            }

            switch platform {
            case "kubernetes":
                skupperCommand = kubeImpl
            case "podman", "docker", "linux":
                skupperCommand = nonKubeImpl
            default:
                return fmt.Errorf("platform %q not supported", platform)
            }

            skupperCommand.NewClient(cmd, args)
            return nil
        },
        Run: func(cmd *cobra.Command, args []string) {
            utils.HandleError(utils.ValidationError, skupperCommand.ValidateInput(args))
            skupperCommand.InputToOptions()
            utils.HandleError(utils.GenericError, skupperCommand.Run())
        },
        PostRun: func(cmd *cobra.Command, args []string) {
            utils.HandleError(utils.GenericError, skupperCommand.WaitUntil())
        },
    }

    return &cmd
}
```

## Validation Patterns

### Comprehensive Validation

Always validate **all** inputs before execution:

```go
func (cmd *CmdTokenIssue) ValidateInput(args []string) error {
    var validationErrors []error

    // 1. Validate CRDs are installed
    _, err := cmd.client.AccessGrants(cmd.namespace).List(context.TODO(), metav1.ListOptions{})
    if err != nil {
        validationErrors = append(validationErrors, utils.HandleMissingCrds(err))
        return errors.Join(validationErrors...) // Early return if CRDs missing
    }

    // 2. Validate arguments
    if len(args) < 1 {
        validationErrors = append(validationErrors, fmt.Errorf("file name must be configured"))
    } else if len(args) > 1 {
        validationErrors = append(validationErrors, fmt.Errorf("only one argument is allowed"))
    } else if args[0] == "" {
        validationErrors = append(validationErrors, fmt.Errorf("file name must not be empty"))
    } else {
        // Validate file path
        if fileInfo, err := os.Stat(args[0]); err == nil && fileInfo.IsDir() {
            validationErrors = append(validationErrors, fmt.Errorf("token file name is a directory"))
        }
        cmd.fileName = args[0]
    }

    // 3. Validate prerequisites (site exists, etc.)
    siteList, err := cmd.client.Sites(cmd.namespace).List(context.TODO(), metav1.ListOptions{})
    if err != nil {
        validationErrors = append(validationErrors, utils.HandleMissingCrds(err))
        return errors.Join(validationErrors...)
    }
    if len(siteList.Items) == 0 {
        validationErrors = append(validationErrors, 
            fmt.Errorf("a site must exist in namespace %s before a token can be created", cmd.namespace))
    }

    // 4. Validate flags
    if cmd.Flags.RedemptionsAllowed < 1 {
        validationErrors = append(validationErrors, fmt.Errorf("number of redemptions is not valid"))
    }

    // 5. Return all errors at once
    return errors.Join(validationErrors...)
}
```

### Validation Best Practices

1. **Collect all errors**: Don't fail on first error - collect all validation errors
2. **Use `errors.Join()`**: Return all errors together for better UX
3. **Validate early**: Check CRDs and prerequisites before detailed validation
4. **Clear messages**: Provide actionable error messages
5. **Store validated values**: Save parsed/validated values in command struct

### Common Validators

Create reusable validators in `internal/utils/validator/`:

```go
type Validator interface {
    Evaluate(value interface{}) (bool, error)
}

type TimeoutValidator struct {
    MinSeconds int
    MaxSeconds int
}

func (v *TimeoutValidator) Evaluate(value interface{}) (bool, error) {
    duration, ok := value.(time.Duration)
    if !ok {
        return false, fmt.Errorf("value must be a duration")
    }
    
    seconds := int(duration.Seconds())
    if seconds < v.MinSeconds {
        return false, fmt.Errorf("timeout must be at least %d seconds", v.MinSeconds)
    }
    if seconds > v.MaxSeconds {
        return false, fmt.Errorf("timeout cannot exceed %d seconds", v.MaxSeconds)
    }
    
    return true, nil
}
```

## Error Handling

### Error Types

```go
// internal/cmd/skupper/common/utils/handle_error.go
type ErrorType int

const (
    GenericError    ErrorType = 1  // General errors
    ValidationError ErrorType = 2  // Input validation errors
)
```

### HandleError Function

```go
func HandleError(errType ErrorType, err error) {
    if err != nil {
        fmt.Println(err)
        syscall.Exit(int(errType))
    }
}
```

**Usage:**

```go
// In command execution
utils.HandleError(utils.ValidationError, cmd.ValidateInput(args))
utils.HandleError(utils.GenericError, cmd.Run())
```

### Special Error Handling: Missing CRDs

```go
func HandleMissingCrds(err error) error {
    if err != nil {
        errMsg := strings.Split(err.Error(), "(")
        if strings.Compare(errMsg[0], "the server could not find the requested resource ") == 0 {
            return errors.New("The Skupper CRDs are not yet installed. To install them, run\n\"kubectl apply -f https://skupper.io/v2/install.yaml\"")
        }
    }
    return err
}
```

### Error Message Guidelines

1. **Be specific**: "file name must not be empty" not "invalid input"
2. **Provide context**: Include namespace, resource name, etc.
3. **Suggest solutions**: "To install CRDs, run..."
4. **Use proper grammar**: Complete sentences with punctuation
5. **Avoid technical jargon**: User-friendly language

## Flags and Configuration

### Flag Definition Pattern

1. **Define constants** in `common/flags.go`:

```go
const (
    FlagNameTimeout  = "timeout"
    FlagDescTimeout  = "raise an error if the operation does not complete in the given period of time (expressed in seconds)."
)
```

2. **Create flag struct**:

```go
type CommandTokenIssueFlags struct {
    RedemptionsAllowed int
    ExpirationWindow   time.Duration
    Timeout            time.Duration
    Cost               string
}
```

3. **Register flags** in command factory:

```go
cmdFlags := common.CommandTokenIssueFlags{}
cmd.Flags().IntVar(&cmdFlags.RedemptionsAllowed, common.FlagNameRedemptionsAllowed, 1, common.FlagDescRedemptionsAllowed)
cmd.Flags().DurationVar(&cmdFlags.Timeout, common.FlagNameTimeout, 60*time.Second, common.FlagDescTimeout)
```

4. **Attach to implementations**:

```go
kubeCommand.Flags = &cmdFlags
nonKubeCommand.Flags = &cmdFlags
```

### Global Flags

Defined in root command:

```go
// internal/cmd/skupper/root/root.go
func init() {
    rootCmd.PersistentFlags().StringVarP(&config.Platform, common.FlagNamePlatform, "p", "", common.FlagDescPlatform)
    rootCmd.PersistentFlags().StringVarP(&SelectedNamespace, common.FlagNameNamespace, "n", "", common.FlagDescNamespace)
    
    platform := common.Platform(config.GetPlatform())
    if platform == common.PlatformKubernetes {
        rootCmd.PersistentFlags().StringVarP(&SelectedContext, common.FlagNameContext, "c", "", common.FlagDescContext)
        rootCmd.PersistentFlags().StringVarP(&KubeConfigPath, common.FlagNameKubeconfig, "", "", common.FlagDescKubeconfig)
    }
}
```

### Flag Best Practices

1. **Use constants**: Never hardcode flag names or descriptions
2. **Provide defaults**: Set sensible default values
3. **Short flags**: Use single-letter shortcuts for common flags (`-n`, `-p`, `-o`)
4. **Consistent naming**: Follow existing patterns (kebab-case)
5. **Type-safe**: Use appropriate types (Duration, Int, Bool, String)

## Testing

### Unit Tests

Test each method of the SkupperCommand interface:

```go
// internal/cmd/skupper/token/kube/token_issue_test.go
func TestValidateInput(t *testing.T) {
    tests := []struct {
        name    string
        args    []string
        flags   *common.CommandTokenIssueFlags
        wantErr bool
        errMsg  string
    }{
        {
            name:    "missing argument",
            args:    []string{},
            wantErr: true,
            errMsg:  "file name must be configured",
        },
        {
            name:    "too many arguments",
            args:    []string{"file1.yaml", "file2.yaml"},
            wantErr: true,
            errMsg:  "only one argument is allowed",
        },
        {
            name:    "valid input",
            args:    []string{"token.yaml"},
            flags:   &common.CommandTokenIssueFlags{RedemptionsAllowed: 1},
            wantErr: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            cmd := &CmdTokenIssue{
                Flags: tt.flags,
                // Mock client
            }
            err := cmd.ValidateInput(tt.args)
            if (err != nil) != tt.wantErr {
                t.Errorf("ValidateInput() error = %v, wantErr %v", err, tt.wantErr)
            }
            if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
                t.Errorf("ValidateInput() error = %v, want error containing %v", err, tt.errMsg)
            }
        })
    }
}
```

### Integration Tests

Test complete command execution:

```go
func TestTokenIssueCommand(t *testing.T) {
    // Set up test environment
    testNamespace := "test-ns"
    testFile := filepath.Join(t.TempDir(), "token.yaml")
    
    // Create mock Kubernetes client
    mockClient := fake.NewSimpleClientset()
    
    // Execute command
    cmd := NewCmdTokenIssue()
    cmd.client = mockClient.SkupperV2alpha1()
    cmd.namespace = testNamespace
    cmd.fileName = testFile
    cmd.Flags = &common.CommandTokenIssueFlags{
        RedemptionsAllowed: 1,
        ExpirationWindow:   15 * time.Minute,
    }
    
    err := cmd.Run()
    if err != nil {
        t.Fatalf("Run() failed: %v", err)
    }
    
    // Verify resource was created
    grants, err := mockClient.SkupperV2alpha1().AccessGrants(testNamespace).List(context.TODO(), metav1.ListOptions{})
    if err != nil {
        t.Fatalf("Failed to list grants: %v", err)
    }
    if len(grants.Items) != 1 {
        t.Errorf("Expected 1 grant, got %d", len(grants.Items))
    }
}
```

## Code Style Guidelines

### General Go Style

Follow standard Go conventions:
- [Effective Go](https://golang.org/doc/effective_go)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)

### Skupper-Specific Style

#### 1. Package Organization

```go
// Group imports: stdlib, external, internal
import (
    "context"
    "fmt"
    "time"
    
    "github.com/spf13/cobra"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    
    "github.com/skupperproject/skupper/internal/cmd/skupper/common"
    "github.com/skupperproject/skupper/internal/kube/client"
)
```

#### 2. Struct Naming

```go
// Command structs: Cmd<CommandName>
type CmdTokenIssue struct {
    client    skupperv2alpha1.SkupperV2alpha1Interface
    CobraCmd  *cobra.Command
    Flags     *common.CommandTokenIssueFlags
    namespace string
    // Internal state (lowercase)
    grantName string
    fileName  string
}
```

#### 3. Error Messages

```go
// Good: Specific, actionable
fmt.Errorf("a site must exist in namespace %s before a token can be created", cmd.namespace)

// Bad: Vague, unhelpful
fmt.Errorf("invalid state")
```

#### 4. Comments

```go
// Good: Explains why, not what
// Wait for AccessGrant to be ready before creating AccessToken
// The controller needs time to generate credentials
err := cmd.WaitUntil()

// Bad: States the obvious
// Call WaitUntil
err := cmd.WaitUntil()
```

#### 5. Function Length

- Keep functions focused and short (< 50 lines ideal)
- Extract complex logic into helper functions
- Use meaningful function names

```go
// Good: Extracted validation logic
func (cmd *CmdTokenIssue) ValidateInput(args []string) error {
    var validationErrors []error
    
    validationErrors = append(validationErrors, cmd.validateCRDs()...)
    validationErrors = append(validationErrors, cmd.validateArguments(args)...)
    validationErrors = append(validationErrors, cmd.validateFlags()...)
    
    return errors.Join(validationErrors...)
}

func (cmd *CmdTokenIssue) validateCRDs() []error {
    // CRD validation logic
}

func (cmd *CmdTokenIssue) validateArguments(args []string) []error {
    // Argument validation logic
}

func (cmd *CmdTokenIssue) validateFlags() []error {
    // Flag validation logic
}
```

## Common Patterns

### Pattern 1: Spinner for Long Operations

```go
func (cmd *CmdTokenIssue) WaitUntil() error {
    waitTime := int(cmd.Flags.Timeout.Seconds())
    return utils.NewSpinnerWithTimeout("Waiting for token status...", waitTime, func() error {
        resource, err := cmd.client.AccessGrants(cmd.namespace).Get(
            context.TODO(),
            cmd.grantName,
            metav1.GetOptions{},
        )
        if err != nil {
            return err
        }
        
        if resource.IsReady() {
            return nil // Success - stop spinner
        }
        
        return fmt.Errorf("not ready") // Keep spinning
    })
}
```

### Pattern 2: Output Formatting

```go
func (cmd *CmdVersion) Run() error {
    version := getVersionInfo()
    
    switch cmd.Flags.Output {
    case "json":
        data, err := json.MarshalIndent(version, "", "  ")
        if err != nil {
            return err
        }
        fmt.Println(string(data))
    case "yaml":
        data, err := yaml.Marshal(version)
        if err != nil {
            return err
        }
        fmt.Println(string(data))
    default:
        // Human-readable format
        fmt.Printf("Version: %s\n", version.Version)
        fmt.Printf("Platform: %s\n", version.Platform)
    }
    
    return nil
}
```

### Pattern 3: Resource Generation (--output flag)

```go
func (cmd *CmdSiteGenerate) Run() error {
    resource := v2alpha1.Site{
        TypeMeta: metav1.TypeMeta{
            APIVersion: "skupper.io/v2alpha1",
            Kind:       "Site",
        },
        ObjectMeta: metav1.ObjectMeta{
            Name: "my-site",
        },
        Spec: v2alpha1.SiteSpec{
            // Populate from flags
        },
    }
    
    switch cmd.Flags.Output {
    case "json":
        data, err := json.MarshalIndent(resource, "", "  ")
        if err != nil {
            return err
        }
        fmt.Println(string(data))
    case "yaml":
        data, err := yaml.Marshal(resource)
        if err != nil {
            return err
        }
        fmt.Println(string(data))
    default:
        return fmt.Errorf("output format must be json or yaml")
    }
    
    return nil
}
```

### Pattern 4: Conditional Platform Flags

```go
func init() {
    rootCmd.PersistentFlags().StringVarP(&config.Platform, "platform", "p", "", "Set the platform")
    rootCmd.PersistentFlags().StringVarP(&SelectedNamespace, "namespace", "n", "", "Set the namespace")
    
    // Kubernetes-only flags
    platform := common.Platform(config.GetPlatform())
    if platform == common.PlatformKubernetes {
        rootCmd.PersistentFlags().StringVarP(&SelectedContext, "context", "c", "", "Set kubeconfig context")
        rootCmd.PersistentFlags().StringVar(&KubeConfigPath, "kubeconfig", "", "Path to kubeconfig")
    }
}
```

### Pattern 5: Wait Strategies

```go
// Wait for specific status
func (cmd *CmdSiteCreate) WaitUntil() error {
    if cmd.Flags.Wait == "none" {
        return nil
    }
    
    waitTime := int(cmd.Flags.Timeout.Seconds())
    return utils.NewSpinnerWithTimeout("Waiting for site...", waitTime, func() error {
        site, err := cmd.client.Sites(cmd.namespace).Get(context.TODO(), cmd.siteName, metav1.GetOptions{})
        if err != nil {
            return err
        }
        
        switch cmd.Flags.Wait {
        case "configured":
            if site.IsConfigured() {
                return nil
            }
        case "ready":
            if site.IsReady() {
                return nil
            }
        }
        
        return fmt.Errorf("waiting for %s status", cmd.Flags.Wait)
    })
}
```

## Additional Resources

- [Cobra Documentation](https://github.com/spf13/cobra)
- [Kubernetes Client-Go](https://github.com/kubernetes/client-go)
- [Skupper API Types](../../pkg/apis/skupper/v2alpha1/)
- [Prerequisites Guide](prerequisites.md)
- [Code Generation Guide](code_generation.md)

## Quick Reference

### Command Checklist

- [ ] Create command group file (`mycommand.go`)
- [ ] Implement Kubernetes version (`kube/mycommand.go`)
- [ ] Implement non-Kubernetes version (`nonkube/mycommand.go`)
- [ ] Add flag definitions to `common/flags.go`
- [ ] Register command in `root/root.go`
- [ ] Implement all SkupperCommand methods
- [ ] Add comprehensive validation
- [ ] Write unit tests
- [ ] Write integration tests
- [ ] Update documentation

### Common Imports

```go
// Cobra
"github.com/spf13/cobra"

// Skupper common
"github.com/skupperproject/skupper/internal/cmd/skupper/common"
"github.com/skupperproject/skupper/internal/cmd/skupper/common/utils"

// Kubernetes client
"github.com/skupperproject/skupper/internal/kube/client"
skupperv2alpha1 "github.com/skupperproject/skupper/pkg/generated/client/clientset/versioned/typed/skupper/v2alpha1"

// Kubernetes types
metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
"github.com/skupperproject/skupper/pkg/apis/skupper/v2alpha1"

// Error handling
"errors"
"fmt"
```

### Useful Commands

```bash
# Build CLI
make build-cmd

# Run tests
go test ./internal/cmd/skupper/...

# Generate mocks
make generate

# Run specific command
./skupper --platform kubernetes token issue test.yaml -n default

## Non-Kubernetes Implementation Deep Dive

### Namespace Concept in Non-Kubernetes

Unlike Kubernetes where namespaces are a first-class cluster resource, in non-Kubernetes platforms (Podman, Docker, Linux), namespaces are **filesystem directories** that organize Skupper resources locally.

#### Namespace Directory Structure

```
$XDG_DATA_HOME/skupper/namespaces/  (or /var/lib/skupper/namespaces/ for root)
├── default/                         # Default namespace
│   ├── input/                       # User-provided resources
│   │   ├── resources/               # Site, Listener, Connector YAMLs
│   │   ├── certs/                   # User-provided certificates
│   │   └── issuers/                 # Certificate issuers
│   ├── runtime/                     # Generated runtime files
│   │   ├── router/                  # Router configuration
│   │   │   └── skrouterd.json      # Router config file
│   │   ├── certs/                   # Generated certificates
│   │   │   ├── skupper-site-ca/
│   │   │   ├── skupper-local-client/
│   │   │   └── [other-certs]/
│   │   ├── resources/               # Processed resources
│   │   └── links/                   # Link tokens
│   └── internal/                    # Internal state
│       ├── platform.yaml            # Platform configuration
│       ├── snapshot/                # Current state snapshot
│       └── scripts/                 # Generated scripts
├── my-namespace/                    # Custom namespace
│   └── [same structure as default]
└── another-namespace/
    └── [same structure]
```

### Path Resolution

#### XDG Base Directory Specification

Skupper follows the [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html):

```go
// pkg/nonkube/api/environment.go

// For non-root users
func GetDataHome() string {
    dataHome, ok := os.LookupEnv("XDG_DATA_HOME")
    if !ok {
        homeDir, _ := os.UserHomeDir()
        dataHome = homeDir + "/.local/share"
    }
    return path.Join(dataHome, "skupper")
}
// Result: ~/.local/share/skupper

// For root user
func GetDataHome() string {
    if os.Getuid() == 0 {
        return "/var/lib/skupper"
    }
    // ... non-root logic
}
// Result: /var/lib/skupper
```

#### Path Types and Functions

```go
// Internal path types
type InternalPath string

const (
    InputIssuersPath      InternalPath = "input/issuers"
    InputCertificatesPath InternalPath = "input/certs"
    InputSiteStatePath    InternalPath = "input/resources"
    RouterConfigPath      InternalPath = "runtime/router"
    CertificatesPath      InternalPath = "runtime/certs"
    IssuersPath           InternalPath = "runtime/issuers"
    RuntimePath           InternalPath = "runtime"
    RuntimeSiteStatePath  InternalPath = "runtime/resources"
    RuntimeTokenPath      InternalPath = "runtime/links"
    InternalBasePath      InternalPath = "internal"
    LoadedSiteStatePath   InternalPath = "internal/snapshot"
    ScriptsPath           InternalPath = "internal/scripts"
)

// Get namespace home directory
func GetHostNamespaceHome(ns string) string {
    dataHome := GetHostDataHome()
    if ns == "" {
        ns = "default"
    }
    return path.Join(dataHome, "namespaces", ns)
}
// Example: ~/.local/share/skupper/namespaces/my-namespace

// Get specific internal path within namespace
func GetInternalOutputPath(namespace string, internalPath InternalPath) string {
    return path.Join(GetDefaultOutputPath(namespace), string(internalPath))
}
// Example: ~/.local/share/skupper/namespaces/my-namespace/runtime/certs
```

### Container vs. Host Paths

When running in a container, paths are different:

```go
func IsRunningInContainer() bool {
    // Check for container marker files
    for _, file := range []string{"/run/.containerenv", "/.dockerenv"} {
        if _, err := os.Stat(file); err == nil {
            return true
        }
    }
    return false
}

func GetDefaultOutputPath(namespace string) string {
    if namespace == "" {
        namespace = "default"
    }
    
    // In container: use /output if mounted
    if IsRunningInContainer() {
        outputStat, err := os.Stat("/output")
        if err == nil && outputStat.IsDir() {
            return path.Join("/output", "namespaces", namespace)
        }
    }
    
    // On host: use XDG data home
    return path.Join(GetDataHome(), "namespaces", namespace)
}
```

**Container Volume Mapping:**
```bash
# User runs on host
podman run -v ~/.local/share/skupper:/output skupper/cli:latest site create

# Inside container, paths resolve to:
# /output/namespaces/default/...

# On host, files appear at:
# ~/.local/share/skupper/namespaces/default/...
```

### SiteState: The Core Data Structure

The `SiteState` struct represents all resources in a namespace:

```go
// pkg/nonkube/api/site_state.go
type SiteState struct {
    SiteId          string
    Site            *v2alpha1.Site
    Listeners       map[string]*v2alpha1.Listener
    Connectors      map[string]*v2alpha1.Connector
    RouterAccesses  map[string]*v2alpha1.RouterAccess
    Grants          map[string]*v2alpha1.AccessGrant
    Links           map[string]*v2alpha1.Link
    Claims          map[string]*v2alpha1.AccessToken
    Certificates    map[string]*v2alpha1.Certificate
    SecuredAccesses map[string]*v2alpha1.SecuredAccess
    Secrets         map[string]*corev1.Secret
    ConfigMaps      map[string]*corev1.ConfigMap
    bundle          bool
}

func (s *SiteState) GetNamespace() string {
    ns := s.Site.GetNamespace()
    if ns == "" {
        return "default"
    }
    return ns
}

func (s *SiteState) SetNamespace(namespace string) {
    if namespace == "" {
        namespace = "default"
    }
    // Update namespace on all resources
    s.Site.SetNamespace(namespace)
    setNamespaceOnMap(s.Listeners, namespace)
    setNamespaceOnMap(s.Connectors, namespace)
    setNamespaceOnMap(s.RouterAccesses, namespace)
    // ... all other resources
}
```

### Resource Persistence

Resources are stored as YAML files in the namespace directory:

```go
func MarshalSiteState(siteState SiteState, outputDirectory string) error {
    // Save Site
    marshal(outputDirectory, "Site", siteState.Site.Name, siteState.Site)
    
    // Save all Listeners
    for name, listener := range siteState.Listeners {
        marshal(outputDirectory, "Listener", name, listener)
    }
    
    // Save all Connectors
    for name, connector := range siteState.Connectors {
        marshal(outputDirectory, "Connector", name, connector)
    }
    
    // ... other resources
}

func marshal(outputDirectory, resourceType, resourceName string, resource interface{}) error {
    fileName := path.Join(outputDirectory, fmt.Sprintf("%s-%s.yaml", resourceType, resourceName))
    file, err := os.Create(fileName)
    if err != nil {
        return err
    }
    defer file.Close()
    
    yaml := json.NewYAMLSerializer(json.DefaultMetaFactory, scheme.Scheme, scheme.Scheme)
    return yaml.Encode(resource.(runtime.Object), file)
}
```

**Example Output:**
```
~/.local/share/skupper/namespaces/my-namespace/runtime/resources/
├── Site-my-site.yaml
├── Listener-backend.yaml
├── Connector-database.yaml
├── RouterAccess-skupper-local.yaml
└── Link-remote-site.yaml
```

### Input File Parsing

Users can provide resources via YAML files:

```go
// internal/nonkube/client/fs/input_parser.go
type InputFileResource struct {
    Site          []v2alpha1.Site
    Listener      []v2alpha1.Listener
    Connector     []v2alpha1.Connector
    RouterAccess  []v2alpha1.RouterAccess
    AccessGrant   []v2alpha1.AccessGrant
    Link          []v2alpha1.Link
    AccessToken   []v2alpha1.AccessToken
    Certificate   []v2alpha1.Certificate
    SecuredAccess []v2alpha1.SecuredAccess
    Secret        []corev1.Secret
}

func ParseInput(namespace string, reader *bufio.Reader, result *InputFileResource) error {
    yamlJsonDecoder := yamlutil.NewYAMLOrJSONDecoder(reader, 1024)
    
    for {
        var rawObj runtime.RawExtension
        err := yamlJsonDecoder.Decode(&rawObj)
        if err == io.EOF {
            break
        }
        
        obj, gvk, err := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme).Decode(rawObj.Raw, nil, nil)
        
        // Parse based on GroupVersionKind
        if v2alpha1.SchemeGroupVersion == gvk.GroupVersion() {
            switch gvk.Kind {
            case "Site":
                var site v2alpha1.Site
                convertTo(obj, &site)
                site.Namespace = namespace  // Override namespace
                result.Site = append(result.Site, site)
            case "Listener":
                var listener v2alpha1.Listener
                convertTo(obj, &listener)
                listener.Namespace = namespace
                result.Listener = append(result.Listener, listener)
            // ... other resource types
            }
        }
    }
    return nil
}
```

**Key Point:** The namespace from the CLI flag (`--namespace` or `-n`) **always overrides** the namespace in the YAML file.

### Platform Configuration

Each namespace stores its platform type:

```go
// internal/nonkube/common/platform.go
type NamespacePlatformLoader struct {
    PathProvider api.InternalPathProvider
    Platform     string `yaml:"platform"`
}

func (s *NamespacePlatformLoader) Load(namespace string) (string, error) {
    if namespace == "" {
        namespace = "default"
    }
    
    internalPath := s.GetPathProvider()(namespace, api.InternalBasePath)
    platformFile, err := os.ReadFile(path.Join(internalPath, "platform.yaml"))
    if err != nil {
        return "", fmt.Errorf("failed to read platform config for namespace %s: %w", namespace, err)
    }
    
    if err = yaml.Unmarshal(platformFile, s); err != nil {
        return "", fmt.Errorf("failed to unmarshal platform config for namespace %s: %w", namespace, err)
    }
    
    return s.Platform, nil
}
```

**platform.yaml example:**
```yaml
platform: podman
```

This allows different namespaces to use different platforms (podman, docker, linux).

### Router Configuration Generation

The SiteState is converted to router configuration:

```go
func (s *SiteState) ToRouterConfig(sslProfileBasePath string, platform string) qdr.RouterConfig {
    if s.SiteId == "" {
        s.SiteId = uuid.New().String()
    }
    
    routerName := s.Site.Name
    routerConfig := qdr.InitialConfig(routerName, s.SiteId, version.Version, !s.IsInterior(), 3)
    
    routerConfig.SiteConfig = &qdr.SiteConfig{
        Name:      routerName,
        Namespace: s.GetNamespace(),
        Platform:  platform,
        Version:   version.Version,
    }
    
    // Add RouterAccess (listeners)
    s.linkAccessMap().DesiredConfig(nil, path.Join(sslProfileBasePath, string(CertificatesPath))).Apply(&routerConfig)
    
    // Add Links (connectors to remote sites)
    s.linkMap(sslProfileBasePath).Apply(&routerConfig)
    
    // Add Bindings (listeners and connectors for services)
    s.bindings(sslProfileBasePath).Apply(&routerConfig)
    
    return routerConfig
}
```

The router config is written to:
```
~/.local/share/skupper/namespaces/my-namespace/runtime/router/skrouterd.json
```

### Certificate Management

Certificates are generated and stored in the namespace:

```go
func (s *SiteState) CreateRouterAccess(name string, port int) {
    tlsCaName := fmt.Sprintf("%s-ca", name)
    tlsServerName := fmt.Sprintf("%s-server", name)
    tlsClientName := fmt.Sprintf("%s-client", name)
    
    // Create RouterAccess resource
    s.RouterAccesses[name] = &v2alpha1.RouterAccess{
        // ... spec
        TlsCredentials: tlsServerName,
        Issuer:         tlsCaName,
    }
    
    // Create CA certificate
    s.Certificates[tlsCaName] = s.newCertificate(tlsCaName, &v2alpha1.CertificateSpec{
        Subject: tlsCaName,
        Hosts:   []string{"127.0.0.1", "localhost"},
        Signing: true,  // This is a CA
    })
    
    // Create server certificate
    s.Certificates[tlsServerName] = s.newCertificate(tlsServerName, &v2alpha1.CertificateSpec{
        Subject: "127.0.0.1",
        Hosts:   []string{"127.0.0.1", "localhost"},
        Ca:      tlsCaName,  // Signed by CA
        Server:  true,
    })
    
    // Create client certificate
    s.Certificates[tlsClientName] = s.newCertificate(tlsClientName, &v2alpha1.CertificateSpec{
        Subject: "127.0.0.1",
        Hosts:   []string{"127.0.0.1", "localhost"},
        Ca:      tlsCaName,
        Client:  true,
    })
}
```

Certificates are written to:
```
~/.local/share/skupper/namespaces/my-namespace/runtime/certs/
├── skupper-local-ca/
│   ├── ca.crt
│   ├── tls.crt
│   └── tls.key
├── skupper-local-server/
│   ├── ca.crt
│   ├── tls.crt
│   └── tls.key
└── skupper-local-client/
    ├── ca.crt
    ├── tls.crt
    └── tls.key
```

### Namespace Isolation

Each namespace is completely isolated:

1. **Separate directories**: No shared state between namespaces
2. **Independent routers**: Each namespace runs its own router instance
3. **Unique Site IDs**: Generated per namespace
4. **Isolated certificates**: No certificate sharing across namespaces

**Example: Multiple Namespaces**
```bash
# Create site in default namespace
skupper site create --platform podman

# Create site in production namespace
skupper site create --platform podman -n production

# Create site in staging namespace
skupper site create --platform podman -n staging

# Result:
~/.local/share/skupper/namespaces/
├── default/      # Independent site
├── production/   # Independent site
└── staging/      # Independent site
```

### CLI Command Implementation Pattern

Here's how a non-Kubernetes command typically works:

```go
// Example: Site Create Command
type CmdSiteCreate struct {
    CobraCmd  *cobra.Command
    Flags     *common.CommandSiteCreateFlags
    namespace string
    siteState *api.SiteState
}

func (cmd *CmdSiteCreate) NewClient(cobraCommand *cobra.Command, args []string) {
    // Get namespace from flag or default
    cmd.namespace = cobraCommand.Flag("namespace").Value.String()
    if cmd.namespace == "" {
        cmd.namespace = "default"
    }
}

func (cmd *CmdSiteCreate) ValidateInput(args []string) error {
    // Check if site already exists in namespace
    namespacePath := api.GetHostNamespaceHome(cmd.namespace)
    if _, err := os.Stat(namespacePath); err == nil {
        // Namespace directory exists - check for existing site
        siteFiles, _ := filepath.Glob(filepath.Join(namespacePath, "runtime/resources/Site-*.yaml"))
        if len(siteFiles) > 0 {
            return fmt.Errorf("site already exists in namespace %s", cmd.namespace)
        }
    }
    return nil
}

func (cmd *CmdSiteCreate) Run() error {
    // Create SiteState
    cmd.siteState = api.NewSiteState(false)
    cmd.siteState.Site = &v2alpha1.Site{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "my-site",
            Namespace: cmd.namespace,
        },
        Spec: v2alpha1.SiteSpec{
            // Populate from flags
        },
    }
    
    // Generate certificates
    cmd.siteState.CreateRouterAccess("skupper-local", 5672)
    if cmd.Flags.EnableLinkAccess {
        cmd.siteState.CreateLinkAccessesCertificates()
    }
    
    // Generate router config
    namespacePath := api.GetHostNamespaceHome(cmd.namespace)
    routerConfig := cmd.siteState.ToRouterConfig(namespacePath, "podman")
    
    // Write router config
    routerConfigPath := api.GetInternalOutputPath(cmd.namespace, api.RouterConfigPath)
    os.MkdirAll(routerConfigPath, 0755)
    configData, _ := qdr.MarshalRouterConfig(routerConfig)
    os.WriteFile(filepath.Join(routerConfigPath, "skrouterd.json"), []byte(configData), 0644)
    
    // Write site state
    resourcesPath := api.GetInternalOutputPath(cmd.namespace, api.RuntimeSiteStatePath)
    api.MarshalSiteState(*cmd.siteState, resourcesPath)
    
    // Write platform config
    internalPath := api.GetInternalOutputPath(cmd.namespace, api.InternalBasePath)
    platformData, _ := yaml.Marshal(map[string]string{"platform": "podman"})
    os.WriteFile(filepath.Join(internalPath, "platform.yaml"), platformData, 0644)
    
    return nil
}
```

### Key Differences from Kubernetes

| Aspect | Kubernetes | Non-Kubernetes |
|--------|-----------|----------------|
| **Namespace** | Cluster resource | Filesystem directory |
| **Resource Storage** | etcd (cluster database) | YAML files on disk |
| **State Management** | Kubernetes API Server | Local filesystem |
| **Client** | Kubernetes client-go | Filesystem operations |
| **Validation** | Admission controllers | Local validation functions |
| **Persistence** | Automatic (etcd) | Manual (write to files) |
| **Isolation** | Cluster-level | Directory-level |
| **Multi-tenancy** | Built-in | Directory separation |

### Common Patterns in Non-Kubernetes Commands

#### Pattern 1: Load Existing State

```go
func LoadSiteState(namespace string) (*api.SiteState, error) {
    siteState := api.NewSiteState(false)
    
    // Load all resource files
    resourcesPath := api.GetInternalOutputPath(namespace, api.RuntimeSiteStatePath)
    files, err := filepath.Glob(filepath.Join(resourcesPath, "*.yaml"))
    if err != nil {
        return nil, err
    }
    
    for _, file := range files {
        data, _ := os.ReadFile(file)
        reader := bufio.NewReader(bytes.NewReader(data))
        
        var resources fs.InputFileResource
        fs.ParseInput(namespace, reader, &resources)
        
        // Merge into siteState
        for _, site := range resources.Site {
            siteState.Site = &site
        }
        for _, listener := range resources.Listener {
            siteState.Listeners[listener.Name] = &listener
        }
        // ... other resources
    }
    
    return siteState, nil
}
```

#### Pattern 2: Update Existing Resource

```go
func (cmd *CmdListenerCreate) Run() error {
    // Load existing state
    siteState, err := LoadSiteState(cmd.namespace)
    if err != nil {
        return err
    }
    
    // Add new listener
    listener := &v2alpha1.Listener{
        ObjectMeta: metav1.ObjectMeta{
            Name:      cmd.listenerName,
            Namespace: cmd.namespace,
        },
        Spec: v2alpha1.ListenerSpec{
            // Populate from flags
        },
    }
    siteState.Listeners[cmd.listenerName] = listener
    
    // Regenerate router config
    routerConfig := siteState.ToRouterConfig(
        api.GetHostNamespaceHome(cmd.namespace),
        "podman",
    )
    
    // Write updated config
    // ... write router config and site state
    
    return nil
}
```

#### Pattern 3: Delete Resource

```go
func (cmd *CmdListenerDelete) Run() error {
    // Load existing state
    siteState, err := LoadSiteState(cmd.namespace)
    if err != nil {
        return err
    }
    
    // Check if listener exists
    if _, exists := siteState.Listeners[cmd.listenerName]; !exists {
        return fmt.Errorf("listener %s not found in namespace %s", cmd.listenerName, cmd.namespace)
    }
    
    // Remove listener
    delete(siteState.Listeners, cmd.listenerName)
    
    // Delete YAML file
    resourcesPath := api.GetInternalOutputPath(cmd.namespace, api.RuntimeSiteStatePath)
    listenerFile := filepath.Join(resourcesPath, fmt.Sprintf("Listener-%s.yaml", cmd.listenerName))
    os.Remove(listenerFile)
    
    // Regenerate router config
    // ... regenerate and write
    
    return nil
}
```

### Debugging Non-Kubernetes Namespaces

**Inspect namespace contents:**
```bash
# List all namespaces
ls ~/.local/share/skupper/namespaces/

# View site configuration
cat ~/.local/share/skupper/namespaces/default/runtime/resources/Site-*.yaml

# View router configuration
cat ~/.local/share/skupper/namespaces/default/runtime/router/skrouterd.json

# Check platform
cat ~/.local/share/skupper/namespaces/default/internal/platform.yaml

# List certificates
ls ~/.local/share/skupper/namespaces/default/runtime/certs/
```

**Common issues:**

1. **Namespace not found**: Directory doesn't exist - site not created
2. **Permission denied**: Check file permissions (should be 0755 for dirs, 0644 for files)
3. **Invalid YAML**: Check resource files for syntax errors
4. **Missing certificates**: Certificate generation failed - check logs

### Best Practices for Non-Kubernetes Commands

1. **Always use GetHostNamespaceHome()**: Don't hardcode paths
2. **Default to "default" namespace**: If namespace is empty
3. **Validate namespace exists**: Before operations
4. **Create directories**: Use `os.MkdirAll()` before writing files
5. **Handle missing files gracefully**: Not all resources may exist
6. **Preserve existing resources**: When updating, don't delete unrelated resources
7. **Use SiteState**: Don't manipulate files directly
8. **Regenerate router config**: After any resource change
9. **Set namespace on all resources**: When parsing input files
10. **Clean up on errors**: Remove partially created resources
