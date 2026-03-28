# 7z GUI Linux

A GUI for 7-zip on Linux with all the advanced features that you are used to.

Uses p7zip installed on your system as the backend. Doesn't require any other dependency to run.

---

Please leave a star if you find it useful!

---

**Note by dev:** The tool is tested by me for my personal use and works pretty nice. But it's still very early in development. If you find any issues, please let me know via [GitHub Issues](https://github.com/Softorage/7z-GUI-Linux/issues).

## Table of Contents

- [Usage](#usage)
- [Features](#features)
- [Screenshots](#screenshots)
- [FAQ](#faq)
- [Contributors](#contributors)
- [Credits](#credits)
- [Other tools](#other-tools)
- [What is Softorage?](#what-is-softorage)
- [License](#license)

## Usage

1. Download from [the official website](https://softorage.github.io/7z-GUI-Linux/).
2. Extract.
3. Right click -> Properties -> Permissions -> Make executable
4. Double click the file to open.

### A few notes:

- You need p7zip installed on your system to use this utility.
- You will be prompted to install p7zip if you do already have it. In that case, you will be required to put your password. You DO NOT need to install p7zip via 7z-GUI-Linux, and can install it on your own.
- Apart from the one specific scenario discussed above (i.e., you not having p7zip already installed), you do not need sudo privilege to use 7z-GUI-Linux, and it is advised that you run it normally (double-click), with least privileges.

## Features

- Compress and Extract 
- Supports:
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
- Offers similar GUI to the Windows counterpart.

## Screenshots

| Description | Screenshot |
| --- | --- |
| Compress | ![Voltage Offsets](/dist/images/screenshots/v0.1.0/Compress.png) |
| Extract | ![Power Limit](/dist/images/screenshots/v0.1.0/Extract.png) |

## FAQ

1. Do you know how to code? Do you use AI to develop this project? Is this vibe-coded?

   Yes. I think, I may kinda know how to code. (-> Sanmay)
   We may use AI when developing this project. If you find any issues, please report them to us. We will try to fix them as soon as possible.
   No.

## Credits

- 7-zip
- p7zip
- Softorage

## Other tools

*Looking for a throttlestop alternative for you Intel CPU on Linux? Try our other tool: [undervolt-go](https://github.com/Softorage/undervolt-go). It comes with all the risks of a system level utility, though.*

## What is Softorage?

Softorage is a software discovery platform with user privacy and safety as its main characteristics. It also allows you to get the software on your computer, but with a distinction. Instead of hosting the packages (which involves risks of package manipulation, and is a well known malware vector), it simply links you to the official developer's website. This helps ensure that you get the software package as the original developers intended.

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
