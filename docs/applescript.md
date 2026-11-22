# AppleScript

Back to the [README](../README.md).

These macOS scripts send files from Finder and Mail and bring notebook content
back to Preview, Notes or the clipboard. Install the helper once, then save
the scripts you want in Script Editor. The scripts were run on macOS against
a private-cloud bench, from Terminal, a LaunchAgent and a Folder Action, with
their dialogs and Automation prompts answered by hand; the Mail script was
compiled but not run.

## Contents

- [Ground rules](#ground-rules)
- [Send the Finder selection](#send-the-finder-selection)
- [A Quick Action](#a-quick-action)
- [A hot folder](#a-hot-folder)
- [Render a notebook and open it in Preview](#render-a-notebook-and-open-it-in-preview)
- [A page on the clipboard](#a-page-on-the-clipboard)
- [Highlights to the clipboard](#highlights-to-the-clipboard)
- [A notebook into Apple Notes](#a-notebook-into-apple-notes)
- [PDF attachments from Mail](#pdf-attachments-from-mail)
- [A daily backup](#a-daily-backup)
- [Library usage in a dialog](#library-usage-in-a-dialog)
- [Permissions](#permissions)

## Ground rules

`do shell script` starts a noninteractive shell with a short PATH. Use the
absolute path reported by `command -v snctl` in Terminal. Homebrew normally
uses `/opt/homebrew/bin/snctl` on Apple silicon or `/usr/local/bin/snctl` on
Intel. Log in to the chosen profile first. A Keychain-backed profile needs
access to the login keychain of the user running the script. See
[configuration](configuration.md).

Save the following as a compiled script named **Supernote.scpt** in
`~/Library/Scripts/` (create the directory if needed). Change the binary,
profile and existing destination folder here. The helper quotes each
argument, preserves line endings and lets a failed command raise an AppleScript
error with its exit status. `previewOnly` previews uploads and keeps local
files in place. Set it to `false` to send them.

```applescript
property executablePath : "/opt/homebrew/bin/snctl"
property profileName : "work"
property destination : "DOCUMENT/Document"
property previewOnly : true

on invoke(argv)
    set cmd to quoted form of executablePath & " -P " & quoted form of profileName
    repeat with arg in argv
        set cmd to cmd & space & quoted form of (contents of arg as text)
    end repeat
    return do shell script cmd without altering line endings
end invoke

on sendFile(f, showPlan)
    set p to POSIX path of f
    if p does not end with ".pdf" and p does not end with ".epub" then return false
    set mode to "--yes"
    if previewOnly then set mode to "--dry-run"
    set planText to my invoke({"put", "--to", destination, mode, "--", p})
    if previewOnly and showPlan then display dialog planText with title "Upload preview"
    return true
end sendFile
```

## Send the Finder selection

Select one or more PDFs or EPUBs and run this from Script Editor or the
Script menu. A conflict stops the run. Earlier successful uploads stay in
the destination.

```applescript
set sn to load script file ((path to scripts folder from user domain as text) & "Supernote.scpt")
tell application "Finder" to set picked to selection as alias list
set n to 0
repeat with f in picked
    if sn's sendFile(contents of f, true) then set n to n + 1
end repeat
if sn's previewOnly then
    display dialog "Checked " & n & " files."
else
    display dialog "Sent " & n & " files."
end if
```

## A Quick Action

In Automator, make a Quick Action that receives files in Finder. Add **Run
AppleScript**, paste the handler, and save as **Send to Supernote**. It is
then available under Quick Actions on the selection. In preview mode the action
discards the dry-run plan. To see it, run the [Finder-selection script](#send-the-finder-selection).

```applescript
on run {input, parameters}
    set sn to load script file ((path to scripts folder from user domain as text) & "Supernote.scpt")
    repeat with f in input
        sn's sendFile((contents of f) as alias, false)
    end repeat
    return input
end run
```

## A hot folder

Create a folder with a subfolder named Sent. Save this compiled script under
`~/Library/Scripts/Folder Action Scripts/`. In Finder use **Folder Actions
Setup** to enable actions and attach the script. Drop finished PDFs and EPUBs
into the folder. A successful upload moves the original to Sent. A conflict
leaves the source in place. In preview mode the folder action leaves the file
in the inbox and discards the dry-run plan. Select the file and run the
[Finder-selection script](#send-the-finder-selection) to see the plan.

```applescript
on adding folder items to inbox after receiving additions
    set sn to load script file ((path to scripts folder from user domain as text) & "Supernote.scpt")
    set archive to (inbox as text) & "Sent:"
    repeat with f in additions
        if sn's sendFile(contents of f, false) and not (sn's previewOnly) then
            tell application "Finder" to move (contents of f) to folder archive
        end if
    end repeat
end adding folder items to
```

## Render a notebook and open it in Preview

Choose a local notebook and a PDF output name. The full build renders the
file before opening Preview. The output can replace an existing PDF.

```applescript
set sn to load script file ((path to scripts folder from user domain as text) & "Supernote.scpt")
set src to POSIX path of (choose file with prompt "Choose a Supernote notebook")
set dst to POSIX path of (choose file name with prompt "Save PDF" default name "Journal.pdf")
sn's invoke({"render", src, "--format", "pdf", "-o", dst})
do shell script "/usr/bin/open -a Preview " & quoted form of dst
```

## A page on the clipboard

Enter a tablet path. Leave the page box empty for the whole notebook. The
answer includes typed boxes and any recognition text stored by the tablet.

```applescript
set sn to load script file ((path to scripts folder from user domain as text) & "Supernote.scpt")
set src to text returned of (display dialog "Notebook path" default answer "NOTE/Note/Work/Journal.note")
set pageNo to text returned of (display dialog "Page number (empty for all)" default answer "")
set argv to {"text", src}
if pageNo is not "" then set argv to argv & {"--page", pageNo}
set the clipboard to sn's invoke(argv)
```

## Highlights to the clipboard

The document path resolves its full-name mark. Markdown includes comments and
coordinates even when passage text is unavailable.

```applescript
set sn to load script file ((path to scripts folder from user domain as text) & "Supernote.scpt")
set src to text returned of (display dialog "Annotated document" default answer "DOCUMENT/Document/Reading/Paper.pdf")
set the clipboard to sn's invoke({"highlights", src, "--format", "markdown"})
```

## A notebook into Apple Notes

Run this to create a note in the default Notes account. Notes accepts HTML
for the body. Escape the extracted text before putting it in a preformatted
block so that angle brackets, ampersands and line breaks survive.

```applescript
set sn to load script file ((path to scripts folder from user domain as text) & "Supernote.scpt")
set src to text returned of (display dialog "Notebook path" default answer "NOTE/Note/Work/Journal.note")
set payload to sn's invoke({"text", src})
set escaped to my replaceText(payload, "&", "&amp;")
set escaped to my replaceText(escaped, "<", "&lt;")
set escaped to my replaceText(escaped, ">", "&gt;")
tell application "Notes" to make new note at default folder of default account with properties {name:src, body:"<pre>" & escaped & "</pre>"}

on replaceText(valueText, needle, replacement)
    set oldDelims to AppleScript's text item delimiters
    set AppleScript's text item delimiters to needle
    set bits to text items of valueText
    set AppleScript's text item delimiters to replacement
    set resultText to bits as text
    set AppleScript's text item delimiters to oldDelims
    return resultText
end replaceText
```

## PDF attachments from Mail

Select messages in Mail and run this script. Each attachment gets a private
staging directory in Downloads. The script ignores names containing path separators and
retains a staged PDF if the upload fails. Correct the failure and send that
file from Finder.

```applescript
set sn to load script file ((path to scripts folder from user domain as text) & "Supernote.scpt")
tell application "Mail" to set messagesToRead to selection
repeat with msg in messagesToRead
    tell application "Mail" to set parts to mail attachments of msg
    repeat with part in parts
        tell application "Mail" to set leaf to name of part
        if leaf ends with ".pdf" and leaf does not contain "/" and leaf does not contain ":" then
            set pattern to POSIX path of (path to downloads folder) & "Supernote-Mail.XXXXXXXX"
            set workDir to do shell script "/usr/bin/mktemp -d " & quoted form of pattern
            set dst to workDir & "/" & leaf
            tell application "Mail" to save part in (POSIX file dst)
            try
                sn's sendFile((POSIX file dst) as alias, false)
            on error errMsg number errNum
                error (errMsg & return & "Attachment retained at " & dst) number errNum
            end try
            do shell script "/bin/rm -f " & quoted form of dst & " && /bin/rmdir " & quoted form of workDir
        end if
    end repeat
end repeat
```

## A daily backup

Save as `~/Library/Scripts/Supernote Backup.scpt`. For a daily LaunchAgent
use the [nightly backup plist](automation.md#nightly-backup) with two
ProgramArguments entries, `/usr/bin/osascript` and the absolute path of the
compiled script. Use a separate backup directory per account.

```applescript
set sn to load script file ((path to scripts folder from user domain as text) & "Supernote.scpt")
set dst to POSIX path of (path to home folder) & "Backups/Supernote"
return sn's invoke({"backup", dst, "--format", "plain"})
```

## Library usage in a dialog

```applescript
set sn to load script file ((path to scripts folder from user domain as text) & "Supernote.scpt")
set report to sn's invoke({"du", "/", "-d", "1", "--format", "plain"})
display dialog report buttons {"OK"} default button "OK" with title "Supernote library usage"
```

## Permissions

macOS grants permissions to the script's host. Run once from the host you
intend to use. Finder, Notes and Mail may prompt for Automation access.
Folder Actions Dispatcher and Automator may need Files and Folders access to
the inbox. Review the host under System Settings → Privacy & Security →
Automation or Files and Folders. A scheduled job cannot answer a Keychain
prompt. Test the backup under the same login before loading its schedule.
