package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestWeatherGetParsesWttrResponse(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/Beijing" || r.URL.Query().Get("format") != "j1" || r.URL.Query().Get("lang") != "en" {
			t.Fatalf("unexpected weather URL: %s", r.URL.String())
		}
		return jsonResponse(200, `{
			"nearest_area":[{"areaName":[{"value":"Beijing"}],"country":[{"value":"China"}],"region":[{"value":"Beijing"}]}],
			"current_condition":[{"temp_C":"21","FeelsLikeC":"20","humidity":"40","windspeedKmph":"12","winddir16Point":"NE","precipMM":"0.0","localObsDateTime":"2026-07-04 10:00 AM","weatherDesc":[{"value":"Sunny"}]}],
			"weather":[
				{"date":"2026-07-04","maxtempC":"25","mintempC":"18","avgtempC":"21","astronomy":[{"sunrise":"05:00 AM","sunset":"07:30 PM"}],"hourly":[{"time":"1200","chanceofrain":"10","weatherDesc":[{"value":"Sunny"}]}]},
				{"date":"2026-07-05","maxtempC":"26","mintempC":"19","avgtempC":"22","hourly":[{"time":"1200","chanceofrain":"20","weatherDesc":[{"value":"Cloudy"}]}]}
			]}`), nil
	})}
	res := NewBuiltinRunner(
		WithWebHTTPClient(client),
		WithWeatherEndpoint("https://wttr.test"),
	).Call(context.Background(), Call{
		Name: WeatherGet,
		Args: json.RawMessage(`{"location":"Beijing","lang":"en","days":1}`),
	})
	if !res.Ok {
		t.Fatalf("weather should succeed: %+v", res)
	}
	payload := decodeToolResult(t, res)
	current := payload["current"].(map[string]any)
	if current["description"] != "Sunny" || current["temp_c"] != float64(21) {
		t.Fatalf("unexpected current weather: %+v", current)
	}
	forecast := payload["forecast"].([]any)
	if len(forecast) != 1 {
		t.Fatalf("forecast should be trimmed to one day: %+v", forecast)
	}
}
