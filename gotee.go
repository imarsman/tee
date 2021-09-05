package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/jwalton/gchalk"
)

const (
	brightGreen = iota
	brightYellow
	brightBlue
	brightRed
	noColour // Can use to default to no colour output
)

// This package is designed to allow for easy intake of standard input and
// logical writing of the contents of standard intput to one or more files
// specified by the invocation of this command. Most of the logic happens at the
// end of the main method. The reading of standard input (not from keyboard) is
// an iterative process, so writes can be done for each file in sequence. If
// there turn out to be concurrency issues channels can be used or some other
// mechanism.

var useColour = true // use colour - defaults to true
var c chan (os.Signal)

// Used to prevent exit on siging with -i option
var doneChannel = make(chan bool)

var readWriter *bufio.ReadWriter
var fileContainer *container

func init() {
	c = make(chan os.Signal, 1)
	fileContainer = newContainer()

	br := bufio.NewReader(os.Stdin)
	bw := bufio.NewWriter(os.Stdout)

	readWriter = bufio.NewReadWriter(br, bw)
}

// fileWriter struct to help manage writing to a file
type fileWriter struct {
	file   *os.File
	writer *bufio.Writer
	active bool
}

// newFileWriter properly initialize a new fileWriter, including catching errors
func newFileWriter(path string, append bool) (*fileWriter, error) {
	s := new(fileWriter)

	var err error
	mode := os.O_APPEND
	if append == false {
		mode = os.O_CREATE
	}
	if _, err = os.Stat(path); err != nil {
		mode = os.O_CREATE
		s.file, err = os.Create(path)
		if err != nil {
			// Something wrong like bad file path
			fmt.Fprintln(os.Stderr, err.Error())
			return nil, err
		}
	} else {
		if append == false {
			s.file, err = os.Create(path)
			if err != nil {
				// Something wrong like bad file path
				fmt.Fprintln(os.Stderr, err.Error())
				return nil, err
			}
		}
	}

	s.active = true
	s.file, err = os.OpenFile(path, mode|os.O_WRONLY, 0644)
	if err != nil {
		// Something wrong like bad file path
		fmt.Fprintln(os.Stderr, err.Error())
		return nil, err
	}
	s.writer = bufio.NewWriter(s.file)

	return s, nil
}

// write write bytes to the bufio.Writer
func (s *fileWriter) write(bytes []byte) error {
	if _, err := s.writer.Write(bytes); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	if err := s.writer.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}

// close close the underlying writer
func (s *fileWriter) close() {
	if err := s.writer.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	s.file.Close()
}

// container holds slice of fileWriters
type container struct {
	fileWriters []*fileWriter
}

// newContainer properly initialize a new container
func newContainer() *container {
	c := new(container)
	c.fileWriters = make([]*fileWriter, 0, 5)

	return c
}

// addFileWriter add a fileWriter to the container's slice
func (c *container) addFileWriter(path string, appendToFile bool) (*fileWriter, error) {
	fileWriter, err := newFileWriter(path, appendToFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Probem obtaining fileWriter for pth", path)
		return nil, err
	}
	c.fileWriters = append(c.fileWriters, fileWriter)

	return fileWriter, nil
}

// write incoming bytes to all fileWriters
func (c *container) write(bytes []byte) {
	fmt.Println("got", string(bytes))
	for _, s := range c.fileWriters {
		s.write(bytes)
	}
}

// close call close on all fileWriters
func (c *container) close() {
	for _, s := range c.fileWriters {
		s.close()
	}
}

func colour(colour int, input ...string) string {
	str := fmt.Sprint(strings.Join(input, " "))
	str = strings.Replace(str, "  ", " ", -1)

	if !useColour {
		return str
	}

	// Choose colour for output or none
	switch colour {
	case brightGreen:
		return gchalk.BrightGreen(str)
	case brightYellow:
		return gchalk.BrightYellow(str)
	case brightBlue:
		return gchalk.BrightBlue(str)
	case brightRed:
		return gchalk.BrightRed(str)
	default:
		return str
	}
}

// printHelp print out simple help output
func printHelp(out *os.File) {
	fmt.Fprintln(out, colour(brightGreen, os.Args[0], "- a simple tee program"))
	fmt.Fprintln(out, "Usage")
	fmt.Fprintln(out, "Takes standard input, saves it to files, and repeats to stdout")
	fmt.Fprintln(out, "Example: tee -i -a file1.txt file2.txt")

	// Prints to stdout
	flag.PrintDefaults()
	os.Exit(0)
}

func main() {

	var helpFlag bool
	flag.BoolVar(&helpFlag, "h", false, "print usage")

	// var noColourFlag bool
	// flag.BoolVar(&noColourFlag, "C", false, "no colour output")
	// useColour = !noColourFlag

	var stdoutFlag bool
	flag.BoolVar(&stdoutFlag, "S", false, "do not forward standard input to standard output")

	// Not using ignore as I don't know what it is good for
	// var ignoreFlag bool
	// flag.BoolVar(&ignoreFlag, "i", false, "ignore interrupt")

	var appendFlag bool
	flag.BoolVar(&appendFlag, "a", false, "append to files if they already exist")

	flag.Parse()

	stdoutFlag = !stdoutFlag

	// args are interpreted as paths
	args := flag.Args()
	fmt.Println("args", args)

	if helpFlag {
		out := os.Stderr
		printHelp(out)
	}

	if len(args) == 0 {
		out := os.Stderr
		fmt.Fprintln(out, colour(brightRed, "No files specified. Exiting with usage information."))
		printHelp(out)
	}

	// Handle interrupt (Control-C) signal. Doing this to do cleanup. The -i flag in the
	// official tee stops interrupt from working. I don't know what this does
	// beyond letting final write happen, which the below goroutine does in all cases.
	signal.Notify(c, os.Interrupt)
	go func() {
		for sig := range c {
			// Block writing to stdErr
			// There may be a better way to allow sig to be defined
			stdErr := os.Stderr
			os.Stderr = nil
			fmt.Fprintln(os.Stderr, colour(brightRed, "got signal", sig.String()))
			time.Sleep(100 * time.Millisecond)
			readWriter.Writer.Flush()
			for _, s := range fileContainer.fileWriters {
				s.close()
			}
			os.Stderr = stdErr
			os.Exit(0)
		}
	}()

	// Use stdin if available, otherwise exit, as stdin is what this is all about.
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
	} else {
		// container := newContainer()
		// Wait on keyboard input. Exit with Control-C.
		// Iterate through file path args to make file writers
		for i := 0; i < len(args); i++ {
			if strings.Contains(args[i], "*") {
				fmt.Fprintln(os.Stderr, "Ignoring globbing path", args[i])
				continue
			}
			_, err := fileContainer.addFileWriter(args[i], appendFlag)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Probem obtaining fileWriter for pth", args[i])
			}
		}
		for {
			// _, err := os.Stdin.Stat()
			// if err != nil {
			// 	fmt.Println("error in stdin")
			// 	break
			// }
			// Read new line of input
			input, isPrefix, err := readWriter.ReadLine()
			if err != nil && err != io.EOF {
				fmt.Fprintln(os.Stderr, err.Error())
				break
			}
			if err != nil {
				fmt.Println(err)
			}
			//  else {
			// 	fmt.Println("stdin is from a terminal")
			// }
			// if err == io.EOF {
			// 	fmt.Println("EOF")
			// }

			if isPrefix {
				fmt.Fprintln(os.Stderr, "line too long")
			}
			// Write line of input to all fileWriters
			for i := 0; i < len(fileContainer.fileWriters); i++ {
				fileWriter := fileContainer.fileWriters[i]
				if fileWriter.active {
					err := fileWriter.write(
						[]byte(
							fmt.Sprintf(
								"%s\n",
								string(input),
							)))
					fileWriter.writer.Flush()
					if err != nil {
						fmt.Fprintln(os.Stderr, err)
						fileWriter.active = false
					}
				}
			}
		}
	}

	// Iterate through file path args
	for i := 0; i < len(args); i++ {
		if strings.Contains(args[i], "*") {
			fmt.Fprintln(os.Stderr, "Ignoring globbing path", args[i])
			continue
		}
		_, err := fileContainer.addFileWriter(args[i], appendFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Probem obtaining fileWriter for pth", args[i])
		}
	}
	if len(fileContainer.fileWriters) == 0 {
		fmt.Fprintln(os.Stderr, "No valid files to save to")
		os.Exit(1)
	}

	buf := make([]byte, 2048)
	count := 0
	// eof := false // eof indicates actual ending of input (plus err.EOF)
	for {
		n, err := readWriter.Read(buf)
		if err != nil && err != io.EOF {
			fmt.Fprintln(os.Stderr, err.Error())
			break
		}
		if n == 0 && err == io.EOF {
			break
		}
		// Send bytes to each file fileWriter
		for i := 0; i < len(fileContainer.fileWriters); i++ {
			fileWriter := fileContainer.fileWriters[i]
			if fileWriter.active {
				err := fileWriter.write(buf[0:n])
				fileWriter.writer.Flush()
				if err != nil {
					fileWriter.active = false
				}
			}
		}
		if stdoutFlag {
			readWriter.Write(buf[0:n])
			// The write method for fileWriter.write does flush.
			readWriter.Flush()
		}
		count++
	}

	// // Shut down as cleanluy as possible on interrupt even without the -i flag
	// readWriter.Flush()
	// for _, s := range fileContainer.fileWriters {
	// 	s.close()
	// }

	// if ignoreFlag {
	// 	// Wait for interupt, or with -i option, kill. Doing it this way allows
	// 	// the interrupt handler to work and for the channel to prevent exit
	// 	// here on interrupt.
	// 	doneChannel <- true
	// }
}
