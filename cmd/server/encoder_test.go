package main

import "testing"

func stubEncoderProbe(t *testing.T, available []string, works map[string]bool) {
	t.Helper()
	avail := make(map[string]bool, len(available))
	for _, name := range available {
		avail[name] = true
	}
	oldAvail, oldWorks := encoderAvailabilityFn, encoderWorksFn
	encoderAvailabilityFn = func(string) map[string]bool { return avail }
	encoderWorksFn = func(_, name string, _ []string) bool { return works[name] }
	t.Cleanup(func() {
		encoderAvailabilityFn = oldAvail
		encoderWorksFn = oldWorks
	})
}

func TestDetectBestEncoderPriority(t *testing.T) {
	t.Setenv("FORCE_CPU", "")
	t.Setenv("ENCODER", "")

	cases := []struct {
		name      string
		available []string
		works     map[string]bool
		want      string
		family    string
	}{
		{
			name:      "NVENC has priority",
			available: []string{"hevc_nvenc", "hevc_qsv", "hevc_amf", "libx265"},
			works:     map[string]bool{"hevc_nvenc": true, "hevc_qsv": true, "hevc_amf": true, "libx265": true},
			want:      "hevc_nvenc",
			family:    "nvenc",
		},
		{
			name:      "QSV when NVENC is unavailable",
			available: []string{"hevc_qsv", "hevc_amf", "libx265"},
			works:     map[string]bool{"hevc_qsv": true, "hevc_amf": true, "libx265": true},
			want:      "hevc_qsv",
			family:    "qsv",
		},
		{
			name:      "AMF when NVENC and QSV are unavailable",
			available: []string{"hevc_amf", "libx265"},
			works:     map[string]bool{"hevc_amf": true, "libx265": true},
			want:      "hevc_amf",
			family:    "amf",
		},
		{
			name:      "CPU ultimate fallback",
			available: []string{"libx265"},
			works:     map[string]bool{"libx265": true},
			want:      "libx265",
			family:    "x265",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stubEncoderProbe(t, c.available, c.works)
			got := detectBestEncoder("ffmpeg")
			if got.Name != c.want || got.Family != c.family {
				t.Errorf("detectBestEncoder = %+v, want %s/%s", got, c.want, c.family)
			}
		})
	}
}

func TestDetectBestEncoderForceCPU(t *testing.T) {
	t.Setenv("FORCE_CPU", "1")
	t.Setenv("ENCODER", "")
	stubEncoderProbe(t, []string{"hevc_nvenc", "libx265"}, map[string]bool{
		"hevc_nvenc": true, "libx265": true,
	})
	got := detectBestEncoder("ffmpeg")
	if got.Name != "libx265" {
		t.Errorf("FORCE_CPU selected %q, want libx265", got.Name)
	}
}

func TestDetectBestEncoderOverride(t *testing.T) {
	t.Setenv("FORCE_CPU", "")
	t.Setenv("ENCODER", "hevc_qsv")
	stubEncoderProbe(t, []string{"hevc_nvenc", "hevc_qsv", "libx265"}, map[string]bool{
		"hevc_nvenc": true, "hevc_qsv": true, "libx265": true,
	})
	got := detectBestEncoder("ffmpeg")
	if got.Name != "hevc_qsv" {
		t.Errorf("ENCODER override selected %q, want hevc_qsv", got.Name)
	}
}

func TestDetectBestEncoderQSVInitArgs(t *testing.T) {
	t.Setenv("FORCE_CPU", "")
	t.Setenv("ENCODER", "")
	stubEncoderProbe(t, []string{"hevc_qsv"}, map[string]bool{})
	encoderWorksFn = func(_, name string, initArgs []string) bool {
		return name == "hevc_qsv" && len(initArgs) > 0
	}
	got := detectBestEncoder("ffmpeg")
	if got.Name != "hevc_qsv" || len(got.InitArgs) == 0 {
		t.Errorf("expected QSV with init args, got %+v", got)
	}
}
