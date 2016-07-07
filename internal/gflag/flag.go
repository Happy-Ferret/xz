// Copyright 2014-2016 Ulrich Kunitz. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Package gflag implements GNU-style command line flag parsing. It
supports the transformation of programs using the Go standard library
flag package. But the package isn't fully compatible. For example the
Flag structure is incomplete and the Var function does have a different
signature.
*/
package gflag

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// CommandLine is the default set of command-line flags parsed from
// os.Args. The top-level functions such as BoolVar, Arg, etc. are
// wrappers for the methods of command line.
var CommandLine = NewFlagSet(os.Args[0], ExitOnError)

// ErrorHandling defines how flag parsing errors are handled.
type ErrorHandling int

// The constants define how errors should be handled.
const (
	ContinueOnError ErrorHandling = iota
	ExitOnError
	PanicOnError
)

// HasArg defines whether a flag argument is required, optional or not
// supported.
type HasArg int

// The constants define whether a flag argument is required, not
// supported or optional.
const (
	RequiredArg HasArg = iota
	NoArg
	OptionalArg
)

// Value is the interface to the value of a specific flag. Set with an
// empty string indicates that the flag is present without an argument.
type Value interface {
	Set(flag, arg string) error
	Get() interface{}
	String() string
}

// Flag represents a single flag.
type Flag struct {
	Name         string
	Shorthands   string
	HasArg       HasArg
	Value        Value
	DefaultValue string
	UsageFlags   string
	Usage        string
}

func (f *Flag) usageFlags() string {
	if f.UsageFlags != "" {
		return f.UsageFlags
	}
	var buf bytes.Buffer
	if f.Shorthands != "" {
		for i, r := range f.Shorthands {
			if i > 0 {
				fmt.Fprint(&buf, ", ")
			}
			fmt.Fprintf(&buf, "-%c", r)
		}
	}
	if f.Name != "" {
		if buf.Len() > 0 {
			fmt.Fprintf(&buf, ", ")
		}
		fmt.Fprint(&buf, "--", f.Name)
		if f.DefaultValue != "" {
			fmt.Fprintf(&buf, "=%s", f.DefaultValue)
		}
	}
	return buf.String()
}

// FlagSet represents a set of option flags.
type FlagSet struct {
	// Provides a custom usage function if set.
	Usage func()

	name          string
	parsed        bool
	formal        map[string]*Flag
	args          []string
	output        io.Writer
	errorHandling ErrorHandling
	preset        bool
}

// Init initializes a flag set variable.
func (f *FlagSet) Init(name string, errorHandling ErrorHandling) {
	f.name = name
	f.errorHandling = errorHandling
}

// NewFlagSet creates a new flag set.
func NewFlagSet(name string, errorHandling ErrorHandling) *FlagSet {
	f := new(FlagSet)
	f.Init(name, errorHandling)
	return f
}

// Arg returns the argument number i after parsing has been successful.
func (f *FlagSet) Arg(i int) string {
	if !(0 <= i && i < len(f.args)) {
		return ""
	}
	return f.args[i]
}

// Arg provides the argument number i after parsing of the command line
// flags.
func Arg(i int) string {
	return CommandLine.Arg(i)
}

// Args returns all arguments after parsing.
func (f *FlagSet) Args() []string { return f.args }

// Args returns all arguments after the command line flags have been
// parsed.
func Args() []string { return CommandLine.args }

// NArg returns the number of remaining arguments after parsing.
func (f *FlagSet) NArg() int { return len(f.args) }

// NArg returns the number of remaining arguments after command line
// parsing.
func NArg() int { return len(CommandLine.args) }

// Parsed returns whether the command line has already been parsed.
func Parsed() bool {
	return CommandLine.parsed
}

// Parsed returns whether the flag set has already been parsed.
func (f *FlagSet) Parsed() bool {
	return f.parsed
}

// Parse parses the command line.
func Parse() {
	// errors are ignored because CommandLine is set on ExitOnError
	CommandLine.Parse(os.Args[1:])
}

// lookupLongOption looks up a long option flag.
func (f *FlagSet) lookupLongOption(name string) (flag *Flag, err error) {
	if len(name) < 2 {
		f.panicf("%s is not a long option", name)
	}
	var ok bool
	if flag, ok = f.formal[name]; !ok {
		return nil, fmt.Errorf("long option %s is unsupported", name)
	}
	if flag.Name != name {
		f.panicf("got %s flag; want %s flag", flag.Name, name)
	}
	return flag, nil
}

// lookupShortOption looks a short option up.
func (f *FlagSet) lookupShortOption(r rune) (flag *Flag, err error) {
	var ok bool
	name := string([]rune{r})
	if flag, ok = f.formal[name]; !ok {
		return nil, fmt.Errorf("short option %s is unsupported", name)
	}
	if !strings.ContainsRune(flag.Shorthands, r) {
		f.panicf("flag supports shorthands %q; but doesn't contain %s",
			flag.Shorthands, name)
	}
	return flag, nil
}

// processExtraFlagArg processes a flag with extra arguments not using
// the form --long-option=arg.
func (f *FlagSet) processExtraFlagArg(name string, flag *Flag, i int) error {
	if flag.HasArg == NoArg {
		// no argument required
		return flag.Value.Set(name, "")
	}
	if i < len(f.args) {
		arg := f.args[i]
		if len(arg) == 0 || arg[0] != '-' {
			err := flag.Value.Set(name, arg)
			switch flag.HasArg {
			case RequiredArg:
				f.removeArg(i)
				return err
			case OptionalArg:
				if err != nil {
					return flag.Value.Set(name, "")
				}
				f.removeArg(i)
				return nil
			}
		}
	}
	// no argument
	if flag.HasArg == RequiredArg {
		return fmt.Errorf("flag %s lacks argument", name)
	}
	// flag.HasArg == OptionalArg
	return flag.Value.Set(name, "")
}

// removeArg removes the arguments at position i from the args field of
// the flag set.
func (f *FlagSet) removeArg(i int) {
	copy(f.args[i:], f.args[i+1:])
	f.args = f.args[:len(f.args)-1]
}

// parseArg parses the argument i.
func (f *FlagSet) parseArg(i int) (next int, err error) {
	arg := f.args[i]
	if len(arg) < 2 || arg[0] != '-' {
		return i + 1, nil
	}
	if arg[1] == '-' {
		// argument starts with --
		f.removeArg(i)
		if len(arg) == 2 {
			// argument is --; remove it and ignore all
			// following arguments
			return len(f.args), nil
		}
		arg = arg[2:]
		flagArg := strings.SplitN(arg, "=", 2)
		flag, err := f.lookupLongOption(flagArg[0])
		if err != nil {
			return i, err
		}
		// case 1: no equal sign
		if len(flagArg) == 1 {
			err = f.processExtraFlagArg(flagArg[0], flag, i)
			return i, err
		}
		// case 2: equal sign
		if flag.HasArg == NoArg {
			err = fmt.Errorf("option %s doesn't support argument",
				arg)
		} else {
			err = flag.Value.Set(flagArg[0], flagArg[1])
		}
		return i, err
	}
	// short options
	f.removeArg(i)
	arg = arg[1:]
	for _, r := range arg {
		flag, err := f.lookupShortOption(r)
		if err != nil {
			return i, err
		}
		err = f.processExtraFlagArg(string([]rune{r}), flag, i)
		if err != nil {
			return i, err
		}
	}
	return i, nil
}

// defaultUsage provides the default usage information.
func defaultUsage(f *FlagSet) {
	if f.name == "" {
		fmt.Fprintf(f.out(), "Usage:\n")
	} else {
		fmt.Fprintf(f.out(), "Usage of %s:\n", f.name)
	}
	f.PrintDefaults()
}

// Usage prints the default usage message.
var Usage = func() {
	fmt.Fprintf(CommandLine.out(), "Usage of %s:\n", os.Args[0])
	PrintDefaults()
}

// usage provides the usage information for the flag set.
func (f *FlagSet) usage() {
	if f.Usage == nil {
		if f == CommandLine {
			Usage()
		} else {
			defaultUsage(f)
		}
	} else {
		f.Usage()
	}
}

// Parse parses the arguments. If an error happens the error is printed
// as well as the usage information.
func (f *FlagSet) Parse(arguments []string) error {
	f.parsed = true
	f.args = arguments
	for i := 0; i < len(f.args); {
		var err error
		i, err = f.parseArg(i)
		if err == nil {
			continue
		}
		fmt.Fprintf(f.out(), "%s: %s\n", f.name, err)
		f.usage()
		switch f.errorHandling {
		case ContinueOnError:
			return err
		case ExitOnError:
			os.Exit(2)
		case PanicOnError:
			panic(err)
		}
	}
	return nil
}

// PrintDefaults prints information about all flags.
func (f *FlagSet) PrintDefaults() {
	panic("TODO")
}

// PrintDefaults prints the information about all command line flags.
func PrintDefaults() {
	CommandLine.PrintDefaults()
}

// out returns a writer. If the field output has not been set os.Stderr
// is returned.
func (f *FlagSet) out() io.Writer {
	if f.output == nil {
		return os.Stderr
	}
	return f.output
}

// SetOutput sets the default output writer for the flag set.
func (f *FlagSet) SetOutput(w io.Writer) {
	f.output = w
}

// panicf prints a formatted error message and panics.
func (f *FlagSet) panicf(format string, values ...interface{}) {
	var msg string
	if f.name == "" {
		msg = fmt.Sprintf(format, values...)
	} else {
		v := make([]interface{}, 1+len(values))
		v[0] = f.name
		copy(v[1:], values)
		msg = fmt.Sprintf("%s "+format, v...)
	}
	fmt.Fprintln(f.out(), msg)
	panic(msg)
}

// setFormal sets the flag with the given name to the flag parameter.
func (f *FlagSet) setFormal(name string, flag *Flag) {
	if name == "" {
		f.panicf("no support for empty name strings")
	}
	if _, alreadythere := f.formal[name]; alreadythere {
		f.panicf("flag redefined: %s", flag.Name)
	}
	if f.formal == nil {
		f.formal = make(map[string]*Flag)
	}
	f.formal[name] = flag
}

// VarP creates a flag with a long and shorthand options.
func (f *FlagSet) VarP(value Value, name, shorthands string, hasArg HasArg, usage string) {
	if len(name) == 1 {
		shorthands += name
		name = ""
	}
	flag := &Flag{
		Name:         name,
		Shorthands:   shorthands,
		Value:        value,
		HasArg:       hasArg,
		DefaultValue: value.String(),
		Usage:        usage,
	}

	if flag.Name == "" && flag.Shorthands == "" {
		f.panicf("flag with no name or shorthands")
	}
	if len(flag.Name) == 1 {
		f.panicf("flag has single character name %q; use shorthands",
			flag.Name)
	}
	if flag.Name != "" {
		f.setFormal(flag.Name, flag)
	}
	if flag.Shorthands != "" {
		for _, r := range flag.Shorthands {
			name := string([]rune{r})
			f.setFormal(name, flag)
		}
	}
}

// VarP creates a flag for the given value for the command line.
func VarP(value Value, name, shorthands string, hasArg HasArg, usage string) {
	CommandLine.VarP(value, name, shorthands, hasArg, usage)
}

// Var creates a flag for the given option name.
func (f *FlagSet) Var(value Value, name string, usage string) {
	f.VarP(value, name, "", RequiredArg, usage)
}

// Var creates a flag for the given option name for the command line.
func Var(value Value, name string, usage string) {
	CommandLine.Var(value, name, usage)
}

// boolValue represents a bool value in the flag.
type boolValue bool

// newBoolValue creates a new Bool Value.
func newBoolValue(val bool, p *bool) *boolValue {
	*p = val
	return (*boolValue)(p)
}

// Get returns the bool value as boolean.
func (b *boolValue) Get() interface{} {
	return bool(*b)
}

// Set sets the bool value to the value provided by the string.
func (b *boolValue) Set(flag, arg string) error {
	if arg == "" {
		*b = true
		return nil
	}
	v, err := strconv.ParseBool(arg)
	if err == nil {
		*b = boolValue(v)
	}
	return err
}

// String returns the boll value as string.
func (b *boolValue) String() string {
	return fmt.Sprintf("%t", *b)
}

// BoolVarP defines a bool flag with specified name, shorthands, default
// value and usage string. The argument p points to a bool variable in
// which to store the value of the flag.
func (f *FlagSet) BoolVarP(p *bool, name, shorthands string, value bool, usage string) {
	f.VarP(newBoolValue(value, p), name, shorthands, OptionalArg, usage)
}

// BoolP defines a bool flag with specified name, shorthands, default
// value and usage string. The return value is the address of a bool
// variable that stores the value of the flag.
func (f *FlagSet) BoolP(name, shorthands string, value bool, usage string) *bool {
	p := new(bool)
	f.BoolVarP(p, name, shorthands, value, usage)
	return p
}

// BoolP defines a bool flag with specified name, shorthands, default
// value and usage string. The return value is the address of a bool
// variable that stores the value of the flag.
func BoolP(name, shorthands string, value bool, usage string) *bool {
	return CommandLine.BoolP(name, shorthands, value, usage)
}

// BoolVarP defines a bool flag with specified name, shorthands, default
// value and usage string. The argument p points to a bool variable in
// which to store the value of the flag.
func BoolVarP(p *bool, name, shorthands string, value bool, usage string) {
	CommandLine.BoolVarP(p, name, shorthands, value, usage)
}

// BoolVar defines a bool flag with specified name, default value and
// usage string. The argument p points to a bool variable in which to
// store the value of the flag.
func (f *FlagSet) BoolVar(p *bool, name string, value bool, usage string) {
	f.VarP(newBoolValue(value, p), name, "", OptionalArg, usage)
}

// BoolVar defines a bool flag with specified name, default value and
// usage string. The argument p points to a bool variable in which to
// store the value of the flag.
func BoolVar(p *bool, name string, value bool, usage string) {
	CommandLine.BoolVar(p, name, value, usage)
}

// Bool defines a bool flag with specified name, default value and
// usage string. The return value is the address of a bool variable that
// stores the value of the flag.
func (f *FlagSet) Bool(name string, value bool, usage string) *bool {
	p := new(bool)
	f.BoolVar(p, name, value, usage)
	return p
}

// Bool defines a bool flag with specified name, default value and
// usage string. The return value is the address of a bool variable that
// stores the value of the flag.
func Bool(name string, value bool, usage string) *bool {
	return CommandLine.Bool(name, value, usage)
}

// intValue stores an integer value.
type intValue int

// newIntValue allocates a new integer value and returns its pointer.
func newIntValue(val int, p *int) *intValue {
	*p = val
	return (*intValue)(p)
}

// Get returns the integer.
func (n *intValue) Get() interface{} {
	return int(*n)
}

// Set sets the integer value.
func (n *intValue) Set(flag, arg string) error {
	if arg == "" {
		(*n)++
		return nil
	}
	v, err := strconv.ParseInt(arg, 0, 0)
	if err == nil {
		*n = intValue(v)
	}
	return err
}

// String represents the integer value as string.
func (n *intValue) String() string {
	return fmt.Sprintf("%d", *n)
}

// CounterVarP defines a counter flag with specified name, shorthands, default
// value and usage string. The argument p points to an integer variable in
// which to store the value of the flag.
func (f *FlagSet) CounterVarP(p *int, name, shorthands string, value int, usage string) {
	f.VarP(newIntValue(value, p), name, shorthands, OptionalArg, usage)
}

// CounterVarP defines a counter flag with specified name, shorthands, default
// value and usage string. The argument p points to an integer variable in
// which to store the value of the flag.
func CounterVarP(p *int, name, shorthands string, value int, usage string) {
	CommandLine.CounterVarP(p, name, shorthands, value, usage)
}

// CounterP defines a counter flag with specified name, shorthands, default
// value and usage string. The return value is the address of an integer
// variable that stores the value of the flag.
func (f *FlagSet) CounterP(name, shorthands string, value int, usage string) *int {
	p := new(int)
	f.CounterVarP(p, name, shorthands, value, usage)
	return p
}

// CounterP defines a counter flag with specified name, shorthands, default
// value and usage string. The return value is the address of an integer
// variable that stores the value of the flag.
func CounterP(name, shorthands string, value int, usage string) *int {
	return CommandLine.CounterP(name, shorthands, value, usage)
}

// CounterVar defines a counter flag with specified name, default value and
// usage string. The argument p points to an integer variable in which to
// store the value of the flag.
func (f *FlagSet) CounterVar(p *int, name string, value int, usage string) {
	f.VarP(newIntValue(value, p), name, "", OptionalArg, usage)
}

// CounterVar defines a counter flag with specified name, default value and
// usage string. The argument p points to an integer variable in which to
// store the value of the flag.
func CounterVar(p *int, name string, value int, usage string) {
	CommandLine.CounterVar(p, name, value, usage)
}

// Counter defines a counter flag with specified name, default value and
// usage string. The return value is the address of an integer variable that
// stores the value of the flag.
func (f *FlagSet) Counter(name string, value int, usage string) *int {
	p := new(int)
	f.CounterVar(p, name, value, usage)
	return p
}

// Counter defines a counter flag with specified name, default value and
// usage string. The return value is the address of an integer variable that
// stores the value of the flag.
func Counter(name string, value int, usage string) *int {
	return CommandLine.Counter(name, value, usage)
}

// IntVarP defines an integer flag with specified name, shorthands, default
// value and usage string. The argument p points to an integer variable in
// which to store the value of the flag.
func (f *FlagSet) IntVarP(p *int, name, shorthands string, value int, usage string) {
	f.VarP(newIntValue(value, p), name, shorthands, RequiredArg, usage)
}

// IntVarP defines an integer flag with specified name, shorthands, default
// value and usage string. The argument p points to an integer variable in
// which to store the value of the flag.
func IntVarP(p *int, name, shorthands string, value int, usage string) {
	CommandLine.IntVarP(p, name, shorthands, value, usage)
}

// IntP defines an integer flag with specified name, shorthands, default
// value and usage string. The return value is the address of an integer
// variable that stores the value of the flag.
func (f *FlagSet) IntP(name, shorthands string, value int, usage string) *int {
	p := new(int)
	f.IntVarP(p, name, shorthands, value, usage)
	return p
}

// IntP defines an integer flag with specified name, shorthands, default
// value and usage string. The return value is the address of an integer
// variable that stores the value of the flag.
func IntP(name, shorthands string, value int, usage string) *int {
	return CommandLine.IntP(name, shorthands, value, usage)
}

// IntVar defines an integer flag with specified name, default value and
// usage string. The argument p points to an integer variable in which to
// store the value of the flag.
func (f *FlagSet) IntVar(p *int, name string, value int, usage string) {
	f.VarP(newIntValue(value, p), name, "", RequiredArg, usage)
}

// IntVar defines an integer flag with specified name, default value and
// usage string. The argument p points to an integer variable in which to
// store the value of the flag.
func IntVar(p *int, name string, value int, usage string) {
	CommandLine.IntVar(p, name, value, usage)
}

// Int defines an integer flag with specified name, default value and
// usage string. The return value is the address of an integer variable that
// stores the value of the flag.
func (f *FlagSet) Int(name string, value int, usage string) *int {
	p := new(int)
	f.IntVar(p, name, value, usage)
	return p
}

// Int defines an integer flag with specified name, default value and
// usage string. The return value is the address of an integer variable that
// stores the value of the flag.
func Int(name string, value int, usage string) *int {
	return CommandLine.Int(name, value, usage)
}

// The stringValue will store a string option.
type stringValue struct {
	p     *string
	value string
}

// newStringValue will create a new stringValue.
func newStringValue(val string, p *string) *stringValue {
	*p = val
	return &stringValue{p, val}
}

// Get returns the string stored in the stringValue.
func (s *stringValue) Get() interface{} {
	return *s.p
}

// Set sets the string value.
func (s *stringValue) Set(flag, arg string) error {
	if arg == "" {
		arg = s.value
	}
	*s.p = arg
	return nil
}

// Update resets the string value to its default.
func (s *stringValue) Update() {
}

// String returns simply the string stored in the value.
func (s *stringValue) String() string {
	return *s.p
}

// StringVarP defines an string flag with specified name, shorthands, default
// value and usage string. The argument p points to a string variable in
// which to store the value of the flag.
func (f *FlagSet) StringVarP(p *string, name, shorthands, value, usage string) {
	f.VarP(newStringValue(value, p), name, shorthands, RequiredArg, usage)
}

// StringVarP defines an string flag with specified name, shorthands, default
// value and usage string. The argument p points to a string variable in
// which to store the value of the flag.
func StringVarP(p *string, name, shorthands, value, usage string) {
	CommandLine.StringVarP(p, name, shorthands, value, usage)
}

// StringP defines a string flag with specified name, shorthands, default
// value and usage string. The return value is the address of a string
// variable that stores the value of the flag.
func (f *FlagSet) StringP(name, shorthands, value, usage string) *string {
	p := new(string)
	f.StringVarP(p, name, shorthands, value, usage)
	return p
}

// StringP defines a string flag with specified name, shorthands, default
// value and usage string. The return value is the address of a string
// variable that stores the value of the flag.
func StringP(name, shorthands, value, usage string) *string {
	return CommandLine.StringP(name, shorthands, value, usage)
}

// StringVar defines a string flag with specified name, default value and
// usage string. The argument p points to a string variable in which to
// store the value of the flag.
func (f *FlagSet) StringVar(p *string, name, value, usage string) {
	f.VarP(newStringValue(value, p), name, "", RequiredArg, usage)
}

// StringVar defines a string flag with specified name, default value and
// usage string. The argument p points to a string variable in which to
// store the value of the flag.
func StringVar(p *string, name, value, usage string) {
	CommandLine.StringVar(p, name, value, usage)
}

// String defines a string flag with specified name, default value and
// usage string. The return value is the address of a string variable that
// stores the value of the flag.
func (f *FlagSet) String(name, value, usage string) *string {
	p := new(string)
	f.StringVar(p, name, value, usage)
	return p
}

// String defines a string flag with specified name, default value and
// usage string. The return value is the address of a string variable that
// stores the value of the flag.
func String(name, value, usage string) *string {
	return CommandLine.String(name, value, usage)
}

// presetValue represents an integer value that can be set with multiple
// flags as -1 ... -9.
type presetValue int

// newPresetValue allocates a new preset value and returns its pointer.
func newPresetValue(p *int, preset int) *presetValue {
	*p = preset
	return (*presetValue)(p)
}

// Get returns the actual preset value as integer.
func (p *presetValue) Get() interface{} {
	return (int)(*p)
}

// Set sets the preset value from an integer string.
func (p *presetValue) Set(flag, arg string) error {
	val, err := strconv.ParseInt(flag, 0, 0)
	*p = presetValue(val)
	return err
}

// String returns the integer representation of the preset value.
func (p *presetValue) String() string {
	return fmt.Sprintf("%d", int(*p))
}

// PresetVar defines a range of preset flags starting at start and
// ending at end. The argument p points to a preset variable in which to
// store the value of the flag.
//
// If start is 1 and end is 9 the flags -1 to -9 will be supported.
func (f *FlagSet) PresetVar(p *int, start, end, value int, usage string) {
	if f.preset {
		f.panicf("flagset %s has already a preset", f.name)
	}
	if !(0 <= start && start <= 9) {
		f.panicf("start must be in range 0 to 9")
	}
	if !(0 <= end && end <= 9) {
		f.panicf("end must be in range 0 to 9")
	}
	if start >= end {
		f.panicf("start must be less than end")
	}
	f.preset = true
	var buf bytes.Buffer
	for i := start; i <= end; i++ {
		fmt.Fprintf(&buf, "%d", i)
	}
	f.VarP(newPresetValue(p, value), "", buf.String(), NoArg, usage)
}

// PresetVar defines a range of preset flags starting at start and
// ending at end. The argument p points to a preset variable in which to
// store the value of the flag.
//
// If start is 1 and end is 9 the flags -1 to -9 will be supported.
func PresetVar(p *int, start, end, value int, usage string) {
	CommandLine.PresetVar(p, start, end, value, usage)
}

// Preset defines a range of preset flags starting at start and
// ending at end. The return value is the address of a preset variable
// in which to store the value of the flag.
//
// If start is 1 and end is 9 the flags -1 to -9 will be supported.
func (f *FlagSet) Preset(start, end, value int, usage string) *int {
	p := new(int)
	f.PresetVar(p, start, end, value, usage)
	return p
}

// Preset defines a range of preset flags starting at start and
// ending at end. The return value is the address of a preset variable
// in which to store the value of the flag.
//
// If start is 1 and end is 9 the flags -1 to -9 will be supported.
func Preset(start, end, value int, usage string) *int {
	return CommandLine.Preset(start, end, value, usage)
}

// Lookup retrieves the flag with the given name or shorthand. The
// method returns nil if no flag can be found.
func (f *FlagSet) Lookup(name string) *Flag {
	return f.formal[name]
}

// Lookup retrieves the flag with the given name or shorthand. The
// method returns nil if no flag can be found.
func Lookup(name string) *Flag {
	return CommandLine.Lookup(name)
}
