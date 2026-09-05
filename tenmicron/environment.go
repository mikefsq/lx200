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

// RefractionCorrection reports whether refraction correction is active (:GREF#) — a
// single status byte, no '#' terminator (see getBoolByte).
func (m *Mount) RefractionCorrection() (bool, error) { return m.getBoolByte(":GREF#") }

// SetSpeedCorrection enables/disables the speed-correction flag (:SSCn#), which
// scales RA/azimuth move speed by cos(dec)⁻¹ for constant on-sky angular speed.
func (m *Mount) SetSpeedCorrection(on bool) (bool, error) {
	return m.Ack(fmt.Sprintf(":SSC%d#", b2i(on)))
}

// SpeedCorrection reports whether the speed-correction flag is active (:GSC#) — a
// single status byte, no '#' terminator (see getBoolByte).
func (m *Mount) SpeedCorrection() (bool, error) { return m.getBoolByte(":GSC#") }

// TemperatureElement identifies a sensor for Temperature (:GTMPn#). Motor, heater and
// electronics-box sensors exist only on special-purpose mounts; the keypad sensors need
// a physical v2 keypad.
type TemperatureElement int

const (
	TempRAAzDriver     TemperatureElement = 1  // RA/azimuth motor driver
	TempDecAltDriver   TemperatureElement = 2  // declination/altitude motor driver
	TempRAAzMotor      TemperatureElement = 7  // RA/azimuth motor
	TempDecAltMotor    TemperatureElement = 8  // declination/altitude motor
	TempElectronicsBox TemperatureElement = 9  // electronics-box sensor (should stay < +65 °C)
	TempKeypadDisplay  TemperatureElement = 11 // keypad v2 display sensor
	TempKeypadPCB      TemperatureElement = 12 // keypad v2 PCB sensor
	TempKeypadCtrl     TemperatureElement = 13 // keypad v2 controller sensor
	TempRAAzHeater     TemperatureElement = 15 // RA/azimuth driver heater
	TempDecAltHeater   TemperatureElement = 16 // declination/altitude driver heater
)

// ErrTemperatureUnavailable is returned by Temperature when the requested sensor's
// reading cannot be read (mount replies "Unavailable#") — e.g. a sensor the mount does
// not have.
var ErrTemperatureUnavailable = errors.New("gotenmicron: temperature reading unavailable")

// Temperature reads the temperature of a mount element in °C (:GTMPn#), or
// ErrTemperatureUnavailable if that sensor is absent. (Firmware ≥ 2.3.0.)
func (m *Mount) Temperature(el TemperatureElement) (float64, error) {
	s, err := m.Get(fmt.Sprintf(":GTMP%d#", int(el)))
	if err != nil {
		return 0, err
	}
	s = strings.TrimSpace(s)
	if s == "Unavailable" {
		return 0, ErrTemperatureUnavailable
	}
	return strconv.ParseFloat(s, 64)
}

// LowTemperatureLimited reports whether a low-temperature condition is limiting the
// mount's maximum slewing performance (:GTMPLT#, firmware ≥ 2.14.8).
func (m *Mount) LowTemperatureLimited() (bool, error) { return m.getBoolByte(":GTMPLT#") }

// MotorOverheatThreshold reads the overheat threshold T_H (°C) for a motor
// (:GTMPOHn#, motor = TempRAAzMotor or TempDecAltMotor): above it, motion stops and the
// heaters turn off. Special-purpose (temperature-sensor) mounts only. (Firmware ≥ 2.7.8.)
func (m *Mount) MotorOverheatThreshold(motor TemperatureElement) (float64, error) {
	if motor != TempRAAzMotor && motor != TempDecAltMotor {
		return 0, fmt.Errorf("gotenmicron: overheat threshold needs a motor element (7 or 8), got %d", int(motor))
	}
	s, err := m.Get(fmt.Sprintf(":GTMPOH%d#", int(motor)))
	if err != nil {
		return 0, err
	}
	if s = strings.TrimSpace(s); s == "Unavailable" { // no motor sensor on this mount
		return 0, ErrTemperatureUnavailable
	}
	return strconv.ParseFloat(s, 64)
}

// MotorTemperatureThresholds reads a motor's three low-temperature thresholds T0,T1,T2
// (°C) (:GTMPTHn#, motor = 7 or 8): below T0 motion stops and heaters power; below T1
// the heaters switch on; above T2 they switch off. Special-purpose mounts only.
// (Firmware ≥ 2.3.0.)
func (m *Mount) MotorTemperatureThresholds(motor TemperatureElement) (t0, t1, t2 float64, err error) {
	if motor != TempRAAzMotor && motor != TempDecAltMotor {
		return 0, 0, 0, fmt.Errorf("gotenmicron: temperature thresholds need a motor element (7 or 8), got %d", int(motor))
	}
	s, err := m.Get(fmt.Sprintf(":GTMPTH%d#", int(motor)))
	if err != nil {
		return 0, 0, 0, err
	}
	s = strings.TrimSpace(s)
	// Special-purpose mounts reply with the three thresholds; a mount without the
	// motor sensors answers "Unavailable" or a single value — treat any non-triple
	// as "feature absent" rather than a parse error.
	if f := strings.Split(s, ","); len(f) == 3 {
		t0, _ = strconv.ParseFloat(strings.TrimSpace(f[0]), 64)
		t1, _ = strconv.ParseFloat(strings.TrimSpace(f[1]), 64)
		t2, _ = strconv.ParseFloat(strings.TrimSpace(f[2]), 64)
		return t0, t1, t2, nil
	}
	return 0, 0, 0, ErrTemperatureUnavailable
}

// SetMotorOverheatThreshold sets the overheat threshold T_H (°C, 0..+80) for a motor
// (:STMPOHn,sTTT.T#, motor = 7 or 8): above it, motion stops and the heaters turn off.
// Special-purpose mounts only. (Firmware ≥ 2.7.8.)
func (m *Mount) SetMotorOverheatThreshold(motor TemperatureElement, tempC float64) error {
	if motor != TempRAAzMotor && motor != TempDecAltMotor {
		return fmt.Errorf("gotenmicron: overheat threshold needs a motor element (7 or 8), got %d", int(motor))
	}
	if tempC < 0 || tempC > 80 {
		return fmt.Errorf("gotenmicron: overheat threshold %.1f°C outside [0, 80]", tempC)
	}
	return must(m.Ack(fmt.Sprintf(":STMPOH%d,%+06.1f#", int(motor), tempC)))
}

// SetMotorTemperatureThresholds sets a motor's three low-temperature thresholds, which
// must satisfy T0 < T1 < T2 and each lie in −100..+40 °C (:STMPTHn,sT0,sT1,sT2#, motor
// = 7 or 8). Special-purpose mounts only. (Firmware ≥ 2.3.)
func (m *Mount) SetMotorTemperatureThresholds(motor TemperatureElement, t0, t1, t2 float64) error {
	if motor != TempRAAzMotor && motor != TempDecAltMotor {
		return fmt.Errorf("gotenmicron: temperature thresholds need a motor element (7 or 8), got %d", int(motor))
	}
	for _, v := range []float64{t0, t1, t2} {
		if v < -100 || v > 40 {
			return fmt.Errorf("gotenmicron: temperature threshold %.1f°C outside [-100, 40]", v)
		}
	}
	if !(t0 < t1 && t1 < t2) {
		return fmt.Errorf("gotenmicron: temperature thresholds must satisfy T0 < T1 < T2 (got %.1f, %.1f, %.1f)", t0, t1, t2)
	}
	return must(m.Ack(fmt.Sprintf(":STMPTH%d,%+06.1f,%+06.1f,%+06.1f#", int(motor), t0, t1, t2)))
}

// ErrWeatherUnavailable is returned by the weather-station getters when no fresh
// datum is available (missing or older than 300 s; mount replies "E#").
var ErrWeatherUnavailable = errors.New("gotenmicron: weather datum unavailable")

// WeatherPressure / WeatherTemperature / WeatherHumidity / WeatherDewPoint read a
// weather-station datum and the age of the reading (:WSP#/:WST#/:WSH#/:WSD#).
// Pressure is hPa, temperature/dew point °C, humidity %. Returns
// ErrWeatherUnavailable when the datum is stale or absent.
func (m *Mount) WeatherPressure() (float64, time.Duration, error) { return m.weather(":WSP#") }

// WeatherTemperature reads the weather-station temperature in °C and the datum age
// (:WST#); see WeatherPressure.
func (m *Mount) WeatherTemperature() (float64, time.Duration, error) { return m.weather(":WST#") }

// WeatherHumidity reads the weather-station relative humidity in % and the datum age
// (:WSH#); see WeatherPressure.
func (m *Mount) WeatherHumidity() (float64, time.Duration, error) { return m.weather(":WSH#") }

// WeatherDewPoint reads the weather-station dew point in °C and the datum age (:WSD#);
// see WeatherPressure.
func (m *Mount) WeatherDewPoint() (float64, time.Duration, error) { return m.weather(":WSD#") }

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

// WeatherAutoUpdateMode reads the refraction-model auto-update mode fed from the
// weather station (:WSG#).
func (m *Mount) WeatherAutoUpdateMode() (WeatherAutoUpdate, error) {
	n, err := m.getInt(":WSG#")
	return WeatherAutoUpdate(n), err
}

// SetWeatherAutoUpdateMode sets the refraction-model auto-update mode fed from the
// weather station (:WSSN#); reports whether the mount accepted it.
func (m *Mount) SetWeatherAutoUpdateMode(mode WeatherAutoUpdate) (bool, error) {
	return m.Ack(fmt.Sprintf(":WSS%d#", int(mode)))
}

// getBool reads a '#'-terminated "0"/"1" reply as a boolean (e.g. :GDH#, :GditS#).
func (m *Mount) getBool(cmd string) (bool, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(s) == "1", nil
}

// getBoolByte reads a bare status byte, with 1 meaning true.
func (m *Mount) getBoolByte(cmd string) (bool, error) {
	b, err := m.AckByte(cmd)
	if err != nil {
		return false, err
	}
	return b == '1', nil
}
