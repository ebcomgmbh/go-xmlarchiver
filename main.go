package main

import (
	"archive/zip"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/fsnotify/fsnotify"
)

const (
	ZIPARCHIV = "xml_archive.zip"
)

var (
	fileMap         = map[string]int{}
	lock            = sync.RWMutex{}
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutex = kernel32.NewProc("CreateMutexW")
)

func CreateMutex(name string) (uintptr, error) {
	ret, _, err := procCreateMutex.Call(
		0,
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(name))),
	)
	switch int(err.(syscall.Errno)) {
	case 0:
		return ret, nil
	default:
		return ret, err
	}
}

func AddFileToZip(zipWriter *zip.Writer, filename string) error {

	fileToZip, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer fileToZip.Close()

	// Get the file information
	info, err := fileToZip.Stat()
	if err != nil {
		return err
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}

	// Using FileInfoHeader() above only uses the basename of the file. If we want
	// to preserve the folder structure we can overwrite this with the full path.
	header.Name = filename

	// Change to deflate to gain better compression
	// see http://golang.org/pkg/archive/zip/#pkg-constants
	header.Method = zip.Deflate

	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, fileToZip)
	if err == nil {
		log.Println("Zipped file: " + filename)
	}
	return err
}

func doZip(ziparchive, file string) error {
	oldFilePath := string('_') + ziparchive
	append := false
	os.Remove(oldFilePath)
	if _, err := os.Stat(ziparchive); err == nil {
		os.Rename(ziparchive, oldFilePath)
		append = true
	}
	targetFile, _ := os.Create(ziparchive)
	defer targetFile.Close()
	targetZipWriter := zip.NewWriter(targetFile)
	defer targetZipWriter.Close()

	if append {
		zipReader, err := zip.OpenReader(oldFilePath)
		if err == nil {
			for _, zipItem := range zipReader.File {
				zipItemReader, _ := zipItem.Open()
				header, _ := zip.FileInfoHeader(zipItem.FileInfo())
				header.Name = zipItem.Name
				targetItem, _ := targetZipWriter.CreateHeader(header)
				_, _ = io.Copy(targetItem, zipItemReader)
			}
			zipReader.Close()
		} else {
			log.Println(err)
		}
	}
	r := AddFileToZip(targetZipWriter, file)
	if r != nil {
		log.Println(r)
	}
	return r
}

func main() {
	_, err := CreateMutex("Global\\EBCOM_XML_ARCHIVER")
	if err != nil {
		log.Println("Program already running")
		os.Exit(1)
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	done := make(chan bool)
	queue := make(chan string)
	go func() {
		for {
			select {
			case ev, ok := <-watcher.Events:
				if !ok {
					return
				}
				//log.Println("event:", ev)
				fileExtension := strings.ToLower(filepath.Ext(ev.Name))
				if fileExtension == ".xml" && ev.Op&fsnotify.Write == fsnotify.Write {
					lock.Lock()
					fileMap[ev.Name] = 5
					lock.Unlock()
				}
			case err := <-watcher.Errors:
				log.Println("error:", err)
			}
		}
	}()
	go func() {
		for {
			filename := <-queue
			if err := doZip(ZIPARCHIV, filename); err != nil {
				log.Println("Retry with:", err)
				queue <- filename
			}
			time.Sleep(1 * time.Second)
		}
	}()

	go func() {
		for {
			time.Sleep(1 * time.Second)
			lock.Lock()
			for file, countdown := range fileMap {
				countdown--
				if countdown == 0 {
					queue <- file
				}
				if countdown >= 0 {
					fileMap[file] = countdown
				}
			}
			lock.Unlock()
		}
	}()

	// Create a buffer to write our archive to.
	err = watcher.Add(".")
	if err != nil {
		log.Fatal(err)
	}
	<-done
	watcher.Close()
	/*
		buf := new(bytes.Buffer)

		// Create a new zip archive.
		w := zip.NewWriter(buf)

		// Add some files to the archive.
		var files = []struct {
			Name, Body string
		}{
			{"readme.txt", "This archive contains some text files."},
			{"gopher.txt", "Gopher names:\nGeorge\nGeoffrey\nGonzo"},
			{"todo.txt", "Get animal handling licence.\nWrite more examples."},
		}
		for _, file := range files {
			f, err := w.Create(file.Name)
			if err != nil {
				log.Fatal(err)
			}
			_, err = f.Write([]byte(file.Body))
			if err != nil {
				log.Fatal(err)
			}
		}

		// Make sure to check the error on Close.
		err := w.Close()
		if err != nil {
			log.Fatal(err)
		}
	*/
}
