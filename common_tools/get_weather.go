package common_tools

//go:generate ../../gen_schema -func=GetWeather -file=get_weather.go -out=../schemas/cached_schemas

// GetWeather is a tool to get the weather in a specific location
func GetWeather(location string) (string, error) {
	return "The weather in " + location + " is sunny", nil
}
