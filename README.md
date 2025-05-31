# UE5 CLI
## Unreal Engine 5 Command Line Interface
This is a command line interface for Unreal Engine 5, designed to simplify the process of building and packaging projects. It provides a set of commands that can be used to automate common tasks, such as building, packaging, and launching projects.

This is very similar to Adam Rehn's [ue4 cli](https://docs.adamrehn.com/ue4cli/overview/introduction-to-ue4cli/), but built with GO as to avoid Python dependencies and to provide a more robust solution.

## Usage
```conosle
UE5 CLI is a command line tool to help build and package Unreal Engine 5 projects.

Usage:
  ue5 [flags]
  ue5 [command]

Available Commands:
  build       Build your Project
  clean       Removes cache and intermediate files from the project
  completion  Generate the autocompletion script for the specified shell
  gen         Generate project files for your Unreal Engine Project
  help        Help about any command
  package     Package your Unreal Engine project for shipping

Flags:
  -d, --debug            Enable debug logging
  -h, --help             help for ue5
  -p, --project string   Path to the project directory (default: current directory)

Use "ue5 [command] --help" for more information about a command.

```

## How it works
This CLI looks at your current directory and searches for a `.uproject` file. If it finds one, it will use that as the project to run commands on. 
This can be overridden by using the `-p` or `--project` flag to specify a different project directory.

This CLI then runs the same commands that you would run but auto calculates the paths to the engine based on your UProject version and the Unreal Engines installed via your Epic Games Launcher manifests.

Thus with multiple versions of Unreal Engine installed, you can run commands on any project without having to specify the engine version or path.