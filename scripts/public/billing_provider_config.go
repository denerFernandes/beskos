package billing

// Config preserves the initialization contract without a payment provider.
type Config struct{}

func FromEnv() Config              { return Config{} }
func (Config) Settings() Settings  { return Settings{} }
func New(Config) (Provider, error) { return ManualProvider{}, nil }
