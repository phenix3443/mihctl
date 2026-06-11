package configgen

func Merge(base, overlay Config) Config {
	result := cloneMap(base)
	for key, value := range overlay {
		existing, ok := result[key]
		if ok {
			result[key] = mergeValue(existing, value)
			continue
		}
		result[key] = DeepCopy(value)
	}
	return result
}

func mergeValue(base, overlay any) any {
	baseMap, baseMapOK := base.(map[string]any)
	overlayMap, overlayMapOK := overlay.(map[string]any)
	if baseMapOK && overlayMapOK {
		return Merge(baseMap, overlayMap)
	}
	return DeepCopy(overlay)
}
