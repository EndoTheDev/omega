package kanban

type Config struct {
	DBPath string `yaml:"db_path"`
}

func Default() Config {
	return Config{DBPath: "kanban.db"}
}
