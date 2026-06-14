package tenmicron

import (
	"errors"
	"testing"
	"time"
)

func TestRefractionSpeedFlags(t *testing.T) {
	m, f := newMount(map[string]string{
		":SREF1#": "1", ":GREF#": "1", // :GREF# replies a bare status byte, no '#'
		":SSC0#": "1", ":GSC#": "0", // :GSC# = bare status byte, no '#'
	})
	if ok, err := m.SetRefractionCorrection(true); err != nil || !ok || f.LastWrite() != ":SREF1#" {
		t.Errorf("SetRefractionCorrection: ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
	if on, err := m.RefractionCorrection(); err != nil || !on {
		t.Errorf("RefractionCorrection = %v, %v; want true", on, err)
	}
	if ok, err := m.SetSpeedCorrection(false); err != nil || !ok || f.LastWrite() != ":SSC0#" {
		t.Errorf("SetSpeedCorrection: ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
	if on, err := m.SpeedCorrection(); err != nil || on {
		t.Errorf("SpeedCorrection = %v, %v; want false", on, err)
	}
}

func TestSetRefractionDatums(t *testing.T) {
	m, f := newMount(map[string]string{":SRPRS1013.2#": "1", ":SRTMP+020.5#": "1"})
	if err := m.SetRefractionPressure(1013.2); err != nil || f.LastWrite() != ":SRPRS1013.2#" {
		t.Errorf("SetRefractionPressure: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetRefractionTemperature(20.5); err != nil || f.LastWrite() != ":SRTMP+020.5#" {
		t.Errorf("SetRefractionTemperature: %v wrote %q", err, f.LastWrite())
	}
}

func TestWeatherDatums(t *testing.T) {
	m, _ := newMount(map[string]string{
		":WSP#": "1013.2,5#",
		":WST#": "-2.5,10#",
		":WSH#": "55.0,3#",
		":WSD#": "E#",
	})
	if v, age, err := m.WeatherPressure(); err != nil || v != 1013.2 || age != 5*time.Second {
		t.Errorf("WeatherPressure = %v, %v, %v; want 1013.2, 5s", v, age, err)
	}
	if v, age, err := m.WeatherTemperature(); err != nil || v != -2.5 || age != 10*time.Second {
		t.Errorf("WeatherTemperature = %v, %v, %v; want -2.5, 10s", v, age, err)
	}
	if v, _, err := m.WeatherHumidity(); err != nil || v != 55.0 {
		t.Errorf("WeatherHumidity = %v, %v; want 55.0", v, err)
	}
	if _, _, err := m.WeatherDewPoint(); !errors.Is(err, ErrWeatherUnavailable) {
		t.Errorf("WeatherDewPoint err = %v; want ErrWeatherUnavailable", err)
	}
}

func TestWeatherAutoUpdate(t *testing.T) {
	m, f := newMount(map[string]string{":WSG#": "2#", ":WSS1#": "1"})
	if v, err := m.WeatherAutoUpdateMode(); err != nil || v != WeatherAutoContinuous {
		t.Errorf("WeatherAutoUpdateMode = %v, %v; want continuous", v, err)
	}
	if ok, err := m.SetWeatherAutoUpdateMode(WeatherAutoWhenIdle); err != nil || !ok || f.LastWrite() != ":WSS1#" {
		t.Errorf("SetWeatherAutoUpdateMode: ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
}
