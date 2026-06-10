package tenmicron

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SetRefraction sets the refraction-model pressure (hPa) and temperature (°C)
// (:SRPRS…#/:SRTMP…#).
func (m *Mount) SetRefraction(pressureHPa, tempC float64) error {
	if err := m.SetRefractionPressure(pressureHPa); err != nil {
		return err
	}
	return m.SetRefractionTemperature(tempC)
}

// SetRefractionPressure sets only the refraction-model pressure in hPa (:SRPRS#).
func (m *Mount) SetRefractionPressure(hPa float64) error {
	return must(m.Ack(fmt.Sprintf(":SRPRS%06.1f#", hPa)))
}

// SetRefractionTemperature sets only the refraction-model temperature in °C
// (:SRTMP#).
func (m *Mount) SetRefractionTemperature(tempC float64) error {
	return must(m.Ack(fmt.Sprintf(":SRTMP%+06.1f#", tempC)))
}

// Refraction reads the refraction-model pressure (hPa) and temperature (°C)
// (:GRPRS#/:GRTMP#).
func (m *Mount) Refraction() (pressureHPa, tempC float64, err error) {
	ps, err := m.Get(":GRPRS#")
	if err != nil {
		return 0, 0, err
	}
	ts, err := m.Get(":GRTMP#")
	if err != nil {
		return 0, 0, err
	}
	pressureHPa, _ = strconv.ParseFloat(strings.TrimSpace(ps), 64)
	tempC, _ = strconv.ParseFloat(strings.TrimSpace(ts), 64)
	return pressureHPa, tempC, nil
}

// SetRefractionCorrection enables/disables refraction correction (:SREFn#).
func (m *Mount) SetRefractionCorrection(on bool) (bool, error) {
	return m.Ack(fmt.Sprintf(":SREF%d#", b2i(on)))
}

// RefractionCorrection reports whether refraction correction is active (:GREF#).
func (m *Mount) RefractionCorrection() (bool, error) { return m.getBool(":GREF#") }

// SetSpeedCorrection enables/disables the speed-correction flag (:SSCn#), which
// scales RA/azimuth move speed by cos(dec)⁻¹ for constant on-sky angular speed.
func (m *Mount) SetSpeedCorrection(on bool) (bool, error) {
	return m.Ack(fmt.Sprintf(":SSC%d#", b2i(on)))
}

// SpeedCorrection reports whether the speed-correction flag is active (:GSC#).
func (m *Mount) SpeedCorrection() (bool, error) { return m.getBool(":GSC#") }

// ErrWeatherUnavailable is returned by the weather-station getters when no fresh
// datum is available (missing or older than 300 s; mount replies "E#").
var ErrWeatherUnavailable = errors.New("gotenmicron: weather datum unavailable")

// WeatherPressure / WeatherTemperature / WeatherHumidity / WeatherDewPoint read a
// weather-station datum and the age of the reading (:WSP#/:WST#/:WSH#/:WSD#).
// Pressure is hPa, temperature/dew point °C, humidity %. Returns
// ErrWeatherUnavailable when the datum is stale or absent.
func (m *Mount) WeatherPressure() (float64, time.Duration, error)    { return m.weather(":WSP#") }
func (m *Mount) WeatherTemperature() (float64, time.Duration, error) { return m.weather(":WST#") }
func (m *Mount) WeatherHumidity() (float64, time.Duration, error)    { return m.weather(":WSH#") }
func (m *Mount) WeatherDewPoint() (float64, time.Duration, error)    { return m.weather(":WSD#") }

func (m *Mount) weather(cmd string) (float64, time.Duration, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return 0, 0, err
	}
	s = strings.TrimSpace(s)
	if s == "E" {
		return 0, 0, ErrWeatherUnavailable
	}
	parts := strings.SplitN(s, ",", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("gotenmicron: bad weather reply %q", s)
	}
	val, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("gotenmicron: weather value %q: %w", parts[0], err)
	}
	age, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	return val, time.Duration(age) * time.Second, nil
}

// WeatherAutoUpdate is the mode for feeding weather-station data into the
// refraction model (:WSG#/:WSSN#).
type WeatherAutoUpdate int

const (
	WeatherAutoOff        WeatherAutoUpdate = 0 // do not update
	WeatherAutoWhenIdle   WeatherAutoUpdate = 1 // update only while not tracking
	WeatherAutoContinuous WeatherAutoUpdate = 2 // update continuously (15 s smoothing)
)

// WeatherAutoUpdateMode / SetWeatherAutoUpdateMode get/set the refraction-model
// auto-update mode from the weather station (:WSG#/:WSSN#).
func (m *Mount) WeatherAutoUpdateMode() (WeatherAutoUpdate, error) {
	n, err := m.getInt(":WSG#")
	return WeatherAutoUpdate(n), err
}

func (m *Mount) SetWeatherAutoUpdateMode(mode WeatherAutoUpdate) (bool, error) {
	return m.Ack(fmt.Sprintf(":WSS%d#", int(mode)))
}

// getBool reads a '#'-terminated "0"/"1" reply as a boolean.
func (m *Mount) getBool(cmd string) (bool, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(s) == "1", nil
}
