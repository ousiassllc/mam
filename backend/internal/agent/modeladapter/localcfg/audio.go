package localcfg

// ParamAudio is the file name of the audio file uploaded to the server (the file of a LoadAudio): the sound of a speech-to-video workflow such as Wan 2.2 S2V
// (Issue #1153). It is a value of a video workflow only. A workflow that has it cannot run without a sound (RequiredAudio), so a request without one is refused
// at once, as one without a first frame is, and a Project's Shot never routes to it (modelroute): mam gives a Shot no sound file to make a clip from.
const ParamAudio Param = "audio"

func init() { videoParams = append(videoParams, ParamAudio) } // videoParams is the list of workflow.go
