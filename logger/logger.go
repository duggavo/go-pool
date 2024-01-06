/*
 * This file is part of go-pool.
 *
 * go-pool is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * go-pool is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with go-pool. If not, see <http://www.gnu.org/licenses/>.
 */

package logger

import (
	"fmt"
	"go-pool/config"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var LogLevel = config.Cfg.LogLevel

var Reset = "\033[0m"
var Red = "\033[31m"
var Green = "\033[32m"
var Yellow = "\033[33m"
var Blue = "\033[34m"
var Purple = "\033[35m"
var Cyan = "\033[36m"
var Gray = "\033[37m"
var White = "\033[97m"
var Bold = "\033[1m"

var f *os.File

func init() {
	var err error
	f, err = os.OpenFile("./master.log", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		panic(err)
	}
}

func writeToFile(t string) {
	if _, err := f.WriteString(t); err != nil {
		panic(err)
	}
}

func getTime() string {
	h := strconv.FormatInt(int64(time.Now().Hour()), 10)
	m := strconv.FormatInt(int64(time.Now().Minute()), 10)
	s := strconv.FormatInt(int64(time.Now().Second()), 10)

	if len(h) == 1 {
		h = "0" + h
	}
	if len(m) == 1 {
		m = "0" + m
	}
	if len(s) == 1 {
		s = "0" + s
	}

	return h + ":" + m + ":" + s
}

func getLogPrefix() string {
	debugInfos := ""

	_, file, line, _ := runtime.Caller(2)
	fileSpl := strings.Split(file, "/")
	debugInfos = strings.Split(fileSpl[len(fileSpl)-1], ".")[0] + ":" + strconv.FormatInt(int64(line), 10)
	for len(debugInfos) < 17 {
		debugInfos = debugInfos + " "
	}

	return fmt.Sprintf("%d-%d-%d %s %s ", time.Now().Year(), time.Now().Month(), time.Now().Day(), getTime(), debugInfos)
}

func DEBUG(a ...any) {
	fmt.Print(getLogPrefix() + Red + Bold + "DEBUG  " + fmt.Sprintln(a...) + Reset)
	writeToFile(getLogPrefix() + "DEBUG  " + fmt.Sprintln(a...))
}

func Debug(a ...any) {
	if LogLevel < 1 {
		return
	}
	fmt.Print(getLogPrefix() + Purple + "DEBUG  " + fmt.Sprintln(a...) + Reset)
	writeToFile(getLogPrefix() + "DEBUG  " + fmt.Sprintln(a...))
}
func Net(a ...any) {
	if LogLevel < 1 {
		return
	}
	fmt.Print(getLogPrefix() + "NET    \033[38;5;75m" + fmt.Sprintln(a...) + Reset)
	writeToFile(getLogPrefix() + "NET    " + fmt.Sprintln(a...))
}
func Dev(a ...any) {
	if LogLevel < 2 {
		return
	}
	fmt.Print(getLogPrefix() + "DEV    " + Purple + fmt.Sprintln(a...) + Reset)
}
func NetDev(a ...any) {
	if LogLevel < 2 {
		return
	}
	fmt.Print(getLogPrefix() + "NETDEV " + "\033[38;5;104m" + fmt.Sprintln(a...) + Reset)
}
func Info(a ...any) {
	fmt.Print(getLogPrefix() + "INFO   " + Cyan + fmt.Sprintln(a...) + Reset)
	writeToFile(getLogPrefix() + "INFO   " + fmt.Sprintln(a...))
}
func Title(a ...any) {
	fmt.Print("       " + Bold + fmt.Sprintln(a...) + Reset)
	writeToFile("       " + fmt.Sprintln(a...))
}
func Warn(a ...any) {
	fmt.Print(getLogPrefix() + "WARN   " + Yellow + fmt.Sprintln(a...) + Reset)
	writeToFile(getLogPrefix() + "WARN   " + fmt.Sprintln(a...))
}
func Error(a ...any) {
	fmt.Print(getLogPrefix() + "ERROR  " + Red + fmt.Sprintln(a...) + Reset)
	writeToFile(getLogPrefix() + "ERROR  " + fmt.Sprintln(a...))
}
func Assert(shouldBeTrue bool) {
	if !shouldBeTrue {
		panic("assertion failed")
	}
}
func Fatal(a ...any) {
	fmt.Print(getLogPrefix()+"FATAL  ", Red)
	for _, v := range a {
		fmt.Print(v, " ")
	}
	writeToFile(getLogPrefix() + "FATAL  " + fmt.Sprintln(a...))

	fmt.Print(Reset)
	os.Exit(1)
}

func IntToString(nmr int) string {
	return strconv.Itoa(nmr)
}
func Uint64ToString(nmr uint64) string {
	return strconv.Itoa(int(nmr))
}
