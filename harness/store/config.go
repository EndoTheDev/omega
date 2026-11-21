package store

type Config struct {
	DBPath string `yaml:"db_path"`
	Home string `yaml:"-"`
}

func Default() Config {
	return Config{DBPath: "omega.db"}
}

const (
	DefaultRetentionDays = 90

	DefaultSummarizeAfterHours = 24
)

type HygieneConfig struct {
	// RetentionDays is the session retention window. 0 = never prune.
	RetentionDays int `yaml:"retention_days"`

	SummarizeAfterHours int `yaml:"summarize_after_hours"`

	SummarizerModel string `yaml:"summarizer_model"`
}

func DefaultHygiene() HygieneConfig {
	return HygieneConfig{
		RetentionDays:       DefaultRetentionDays,
		SummarizeAfterHours: DefaultSummarizeAfterHours,
	}
}
