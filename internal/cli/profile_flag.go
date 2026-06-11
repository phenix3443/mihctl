package cli

var loadDefaultProfile = func() string {
	env, err := loadEnv()
	if err != nil {
		return ""
	}
	return env.DefaultProfile
}

func defaultProfileFlagValue() string {
	return loadDefaultProfile()
}
