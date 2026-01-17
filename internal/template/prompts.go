package template

import (
	"fmt"

	"github.com/AlecAivazis/survey/v2"
)

// PromptConfig prompts the user for configuration options
func PromptConfig(cfg *Config) error {
	// Prompt for language if not set
	if cfg.Lang == "" {
		lang, err := promptLanguage()
		if err != nil {
			return err
		}
		cfg.Lang = lang
	}

	// Prompt for database
	if cfg.Database == "" {
		db, err := promptDatabase()
		if err != nil {
			return err
		}
		cfg.Database = db
	}

	// Prompt for port (only for API type)
	if cfg.Type == TemplateAPI && cfg.Port == 0 {
		port, err := promptPort()
		if err != nil {
			return err
		}
		cfg.Port = port
	}

	return nil
}

func promptLanguage() (Language, error) {
	options := []string{
		"go (net/http + chi router)",
		"node (Express.js)",
		"python (FastAPI)",
	}

	var selected string
	prompt := &survey.Select{
		Message: "Select language/framework:",
		Options: options,
	}

	if err := survey.AskOne(prompt, &selected); err != nil {
		return "", err
	}

	// Map selection to language
	switch selected {
	case options[0]:
		return LangGo, nil
	case options[1]:
		return LangNode, nil
	case options[2]:
		return LangPython, nil
	default:
		return LangGo, nil
	}
}

func promptDatabase() (string, error) {
	options := []string{
		"none",
		"postgres",
		"redis",
		"mongodb",
		"mysql",
	}

	var selected string
	prompt := &survey.Select{
		Message: "Include database?",
		Options: options,
		Default: "none",
	}

	if err := survey.AskOne(prompt, &selected); err != nil {
		return "", err
	}

	if selected == "none" {
		return "", nil
	}
	return selected, nil
}

func promptPort() (int, error) {
	var port int
	prompt := &survey.Input{
		Message: "Port number:",
		Default: "8080",
	}

	if err := survey.AskOne(prompt, &port); err != nil {
		return 8080, err
	}

	if port == 0 {
		port = 8080
	}
	return port, nil
}

// ConfirmOverwrite prompts the user to confirm overwriting existing files
func ConfirmOverwrite(path string) (bool, error) {
	var confirm bool
	prompt := &survey.Confirm{
		Message: fmt.Sprintf("Directory %q already exists. Overwrite?", path),
		Default: false,
	}

	if err := survey.AskOne(prompt, &confirm); err != nil {
		return false, err
	}

	return confirm, nil
}
