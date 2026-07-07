# 7GL
7z GUI Linux

A GUI for 7-zip on Linux with all the advanced features that you are used to, and more. Doesn't require any dependency to run other than the 7-zip backend.

Uses either of the below as backend:
  - 7zz (7-zip for Linux)
  - 7zzs (statically linked 7-zip for Linux): You will find this alongside 7z-GUI-Linux
  - p7zip: Older community developed version. Often installed on many systems as a default. 

---

Please leave a star if you find it useful!

---

**Note by dev:**
- The tool is tested by me for my personal use and works pretty nice. But it's still very early in development. If you find any issues, please let me know via [GitHub Issues](https://github.com/Softorage/7z-GUI-Linux/issues).
- Although I am releasing the tool under Softorage (which receives fewer [visits](https://softorage.com/blog/2026/02/softorage-by-the-numbers/) than you might think), it's still me working in my free time. Thank you for understanding.

## Table of Contents

- [Screenshots](#screenshots)
- [Features](#features)
- [Usage](#usage)
- [Building](#Building)
- [FAQ](#faq)
- [Credits](#credits)
- [Other tools](#other-tools)
- [What is Softorage?](#what-is-softorage)
- [License](#license)


## Screenshots

| Description | Screenshot |
| --- | --- |
| Explorer | ![Explorer](/dist/images/screenshots/v0.3.0/Explorer.png) |
| Clipboard | ![Clipboard](/dist/images/screenshots/v0.3.0/Clipboard.png) |
| Compress | ![Compress](/dist/images/screenshots/v0.3.0/Compress1.png) <br> ![Compress](/dist/images/screenshots/v0.3.0/Compress2.png) |
| Extract | ![Extract](/dist/images/screenshots/v0.3.0/Extract.png) |
| Checksum | ![Checksum](/dist/images/screenshots/v0.3.0/Checksum1.png) <br> ![Checksum](/dist/images/screenshots/v0.3.0/Checksum2.png) |
| Operations History (Status) | ![Operations History](/dist/images/screenshots/v0.3.0/OpHistory_Status.png) |
| Log (Status) | ![Log](/dist/images/screenshots/v0.3.0/Log_Status.png) |

## Features

- Basic Explorer with basic file operations
- Archive operations
   - Browse through archives
   - Copy/cut from folder to archive, archive to folder, and archive to archive
- System independent descriptive clipboard
- Compress and Extract 
- Supports:
   - Multiple archive formats
   - Compression methods
   - Compression level
   - Dictionary size
   - Word size
   - Block size
   - CPU threads
   - Update mode
   - SFX (self-extracting) archive (.exe)
   - Compress shared files
   - Split to volumes
   - Encryption settings
- Provides introductory information for various input options
- Logging
- Offers familiar GUI to the Windows counterpart.


## Usage

### Install/Uninstall/Update

1. Download 7z-GUI-Linux  as appropriate for you CPU architecture from [the official website](https://softorage.github.io/7z-GUI-Linux/).
2. Extract.
3. If you want to use it as a portable tool without installation:
   1. Having extracted, you should now have the `7z-GUI-Linux` file along with other files.
   2. Simply make `7z-GUI-Linux` executable => right click `7z-GUI-Linux`, go to Properties, and in the Permissions tab, tick 'Make executable'.
   3. Double click `7z-GUI-Linux` to launch!
4. It is recommended to install the tool in userspace (i.e. without `sudo`) instead of system install (i.e. with `sudo`). Nevertheless, you can safely install, update or uninstall under `sudo` privileges as well.
5. To install, update or uninstall:
   - Install: 
      1. In the the extracted folder, find `install.sh`.
      2. Simply make `install.sh` executable => right click `install.sh`, go to Properties, and in the Permissions tab, tick 'Make executable'.
      3. Double click `install.sh` to install!
   - Update:
      1. In the the extracted folder, find `update.sh`.
      2. Simply make `update.sh` executable => right click `update.sh`, go to Properties, and in the Permissions tab, tick 'Make executable'.
      3. Double click `update.sh` to update!
   - Uninstall:
      1. In the the extracted folder, find `uninstall.sh`.
      2. Simply make `uninstall.sh` executable => right click `uninstall.sh`, go to Properties, and in the Permissions tab, tick 'Make executable'.
      3. Double click `uninstall.sh` to uninstall!

Note: If you face any crashes, please run debug build through command line, and recreate the circumstances leading to crash. Share the coutput in command line when opening the issue on GitHub.

### Basic guide

1. How to add files to existing archive?
   - Simply copy the file(s) or folder(s) you wish to add to an archive
   - Open the archive in which you wish to add the files
   - Paste into the archive

## Building

- Make sure you have ssh set up.
- Clone the repo
   ```
   git clone git@github.com:Softorage/7z-GUI-Linux.git
   ```
- Move into the directory and run the build command.
   ```
   cd 7z-GUI-Linux
   go build
   ```

**Note:**  
When making changes to the code, it is recommended to run a set of standard Go commands.
```
# Format the code.
go fmt

# Very handy in case a library that is not imported is used.
# `goimports` updates the import statements for you and formats the code.
# -w writes the changes to the files.
# . runs it on the current directory and subdirectories.
goimports -w .

# Check for common mistakes like unreachable code, printf format mismatches, and shadowed variables with `go vet`.
# You can also run `golangci-lint run` for a comprehensive check. It is a "meta-linter" that runs go vet, staticcheck, and dozens of other tools simultaneously.
go vet .

# Remove unused dependencies and adds missing ones to your go.mod and go.sum files.
go mod tidy
```

## FAQ

1. Do you know how to code? Do you use AI to develop this project? Is this vibe-coded?

   Yes. I think, I may kinda know how to code. I am fairly confident that I understand the code I maintain (I keep forgetting though). Sometimes, there do appear parts of code (often via LLMs) that work and I don't quite understand how (and I have to ask to understand). But hey, that was the case even in StackOverflow days. I guess I'm dumb, just not enough to constantly keep messing the code. (-> Sanmay)  
   We may use AI when developing this project. If you find any issues, please report them to us. We will try to fix them as soon as possible.  
   No.

## Credits

- 7-zip
- p7zip
- Softorage

## Other tools

*Looking for a throttlestop alternative for your Intel CPU on Linux? Try our other tool: [undervolt-go](https://github.com/Softorage/undervolt-go). It comes with all the risks of a system level utility, though.*

## What is Softorage?

Softorage is a software discovery platform that takes user trust and safety very seriously. It allows you to get the software on your computer, but with a distinction. Instead of hosting the packages (which involves risks of package manipulation, and is a well known malware vector), it simply links you to the official developer's website. This helps ensure that you get the software package as the original developers intended.

## License

This project is licensed under the GNU General Public License v3.0. See the [LICENSE.txt](LICENSE.txt) file for details.

### NO WARRANTY

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
