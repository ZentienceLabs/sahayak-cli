import os

filepath = 'core/config/config.go'
with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

target = '	if v := os.Getenv("SAHAYAK_CLOUD_PROVIDER"); v != "" {\n		c.CloudProvider = v\n	}\n	return c\n}'

replacement = """	if v := os.Getenv("SAHAYAK_CLOUD_PROVIDER"); v != "" {
		c.CloudProvider = v
	}
	
	// Load from JSON file if exists
	home, _ := os.UserHomeDir()
	if home != "" {
		if b, err := os.ReadFile(home + "/.sahayak/config.json"); err == nil {
			import_json := true
			// We should unmarshal it, but we need the encoding/json package.
			// Let's add that to the imports.
		}
	}
	return c
}
"""
