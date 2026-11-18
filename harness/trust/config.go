package trust

type Config struct {
	Home string
}

func Default() Config {
	return Config{}
}
