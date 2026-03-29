package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gox-lang/gox/lexer"
	"github.com/gox-lang/gox/parser"
	"github.com/gox-lang/gox/token"
	"github.com/gox-lang/gox/transformer"
)

func main() {
	args := os.Args[1:]

	if len(args) < 1 {
		fmt.Println("Usage: goxrun <source.gox> [args...]")
		fmt.Println("\nDirectly run .gox files without manual compilation steps.")
		fmt.Println("\nExample:")
		fmt.Println("  goxrun test/fx_component.gox")
		fmt.Println("  goxrun test/demo_counter.gox")
		os.Exit(1)
	}

	srcFile := args[0]
	runArgs := args[1:]

	// Check if file exists
	if _, err := os.Stat(srcFile); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: File not found: %s\n", srcFile)
		os.Exit(1)
	}

	// Read source file
	src, err := os.ReadFile(srcFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("🔍 Parsing %s...\n", filepath.Base(srcFile))

	// Lexical analysis
	l := lexer.New(string(src))
	tokens := l.Tokens()

	// Check for lexer errors (EOF token at the end is normal)
	for _, tok := range tokens {
		if tok.Kind == token.EOF {
			break
		}
	}

	// Parsing
	p := parser.New(string(src))
	prog := p.ParseProgram()

	// Check for parser errors
	if len(p.Errors()) > 0 {
		fmt.Fprintln(os.Stderr, "❌ Parser Errors:")
		for _, err := range p.Errors() {
			fmt.Fprintf(os.Stderr, "  %s\n", err)
		}
		os.Exit(1)
	}

	fmt.Println("✓ Parsing successful")

	// Transform to Go code
	t := transformer.New()
	goCode := t.Transform(prog)

	fmt.Println("✓ Code generation successful")

	// Use the same directory as the source file for build (to use existing go.mod)
	srcDir := filepath.Dir(srcFile)
	baseName := strings.TrimSuffix(filepath.Base(srcFile), filepath.Ext(srcFile))
	// Use fixed temp filename to avoid accumulation (overwrite previous runs)
	tempFile := filepath.Join(srcDir, fmt.Sprintf("%s_gox_temp.go", baseName))

	err = os.WriteFile(tempFile, []byte(goCode), 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing temp file: %v\n", err)
		os.Exit(1)
	}

	// Ensure cleanup on exit
	defer func() {
		os.Remove(tempFile)
		// Also remove generated exe if exists
		exeFile := strings.TrimSuffix(tempFile, ".go") + ".exe"
		os.Remove(exeFile)
	}()

	fmt.Printf("✓ Temporary file: %s\n", filepath.Base(tempFile))

	// Find go executable
	goExe := findGoExecutable()
	if goExe == "" {
		fmt.Fprintln(os.Stderr, "❌ Error: Go executable not found")
		fmt.Fprintln(os.Stderr, "Please ensure Go is installed and in PATH, or set GOX_GO_EXECUTABLE environment variable")
		os.Exit(1)
	}

	// Run go mod tidy first to ensure all dependencies are available
	fmt.Println("📦 Running go mod tidy...")
	tidyCmd := exec.Command(goExe, "mod", "tidy")
	tidyCmd.Dir = srcDir
	tidyOutput, err := tidyCmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Go mod tidy warning: %v\n", err)
		if len(tidyOutput) > 0 {
			fmt.Fprintf(os.Stderr, "%s\n", string(tidyOutput))
		}
		// Continue anyway, it might just be warnings
	}

	// Build the generated Go code
	fmt.Println("🔨 Building...")
	exeFile := strings.TrimSuffix(tempFile, ".go") + ".exe"
	buildCmd := exec.Command(goExe, "build", "-o", exeFile, filepath.Base(tempFile))
	buildCmd.Dir = srcDir
	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		fmt.Fprintln(os.Stderr, "❌ Build error:")
		fmt.Fprintln(os.Stderr, string(buildOutput))
		os.Exit(1)
	}

	fmt.Println("✓ Build successful")

	// Run the compiled program
	fmt.Println("🚀 Running...")
	runCmd := exec.Command(exeFile)
	runCmd.Args = append([]string{exeFile}, runArgs...)
	runCmd.Dir = srcDir
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	runCmd.Stdin = os.Stdin

	err = runCmd.Run()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			os.Exit(exitError.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "❌ Runtime error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n✓ Execution completed")
}

func findGoExecutable() string {
	// Check environment variable first
	if goExe := os.Getenv("GOX_GO_EXECUTABLE"); goExe != "" {
		return goExe
	}

	// Try the project's Go runtime first (most reliable)
	// Get absolute path to avoid issues when changing working directory
	cwd, err := os.Getwd()
	if err == nil {
		projectGo := filepath.Join(cwd, "runtime", "go", "bin", "go.exe")
		if _, err := os.Stat(projectGo); err == nil {
			return projectGo
		}
	}

	// Try system PATH
	if path, err := exec.LookPath("go.exe"); err == nil {
		return path
	}
	if path, err := exec.LookPath("go"); err == nil {
		return path
	}

	return ""
}
