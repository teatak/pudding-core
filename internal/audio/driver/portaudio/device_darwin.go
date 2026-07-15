package portaudio

/*
#cgo LDFLAGS: -framework CoreAudio

#include <CoreAudio/CoreAudio.h>
#include <stdatomic.h>

static atomic_int caDeviceChanged = 0;
static atomic_uint lastDefaultInputID  = 0;
static atomic_uint lastDefaultOutputID = 0;

static AudioObjectID queryDefaultDeviceID(AudioObjectPropertySelector selector) {
	AudioObjectPropertyAddress addr;
	addr.mSelector = selector;
	addr.mScope = kAudioObjectPropertyScopeGlobal;
	addr.mElement = kAudioObjectPropertyElementMain;
	AudioObjectID id = 0;
	UInt32 size = sizeof(id);
	OSStatus err = AudioObjectGetPropertyData(kAudioObjectSystemObject, &addr, 0, NULL, &size, &id);
	if (err != noErr) return 0;
	return id;
}

static OSStatus caDeviceChangeCallback(
	AudioObjectID inObjectID,
	UInt32 inNumberAddresses,
	const AudioObjectPropertyAddress *inAddresses,
	void *inClientData
) {
	(void)inObjectID; (void)inClientData;
	int realChange = 0;
	for (UInt32 i = 0; i < inNumberAddresses; i++) {
		AudioObjectPropertySelector sel = inAddresses[i].mSelector;
		if (sel == kAudioHardwarePropertyDefaultInputDevice) {
			AudioObjectID cur = queryDefaultDeviceID(sel);
			unsigned int prev = atomic_exchange(&lastDefaultInputID, (unsigned int)cur);
			if (cur != 0 && (unsigned int)cur != prev) realChange = 1;
		} else if (sel == kAudioHardwarePropertyDefaultOutputDevice) {
			AudioObjectID cur = queryDefaultDeviceID(sel);
			unsigned int prev = atomic_exchange(&lastDefaultOutputID, (unsigned int)cur);
			if (cur != 0 && (unsigned int)cur != prev) realChange = 1;
		}
	}
	if (realChange) atomic_store(&caDeviceChanged, 1);
	return noErr;
}

void installCADeviceListener() {
	atomic_store(&lastDefaultInputID,  (unsigned int)queryDefaultDeviceID(kAudioHardwarePropertyDefaultInputDevice));
	atomic_store(&lastDefaultOutputID, (unsigned int)queryDefaultDeviceID(kAudioHardwarePropertyDefaultOutputDevice));

	AudioObjectPropertyAddress addr;
	addr.mScope = kAudioObjectPropertyScopeGlobal;
	addr.mElement = kAudioObjectPropertyElementMain;

	addr.mSelector = kAudioHardwarePropertyDefaultInputDevice;
	AudioObjectAddPropertyListener(kAudioObjectSystemObject, &addr, caDeviceChangeCallback, NULL);

	addr.mSelector = kAudioHardwarePropertyDefaultOutputDevice;
	AudioObjectAddPropertyListener(kAudioObjectSystemObject, &addr, caDeviceChangeCallback, NULL);
}

int checkAndResetCADeviceChanged() {
	return atomic_exchange(&caDeviceChanged, 0);
}

// Returns 1 for Bluetooth, 0 for a known non-Bluetooth device, and -1 when
// CoreAudio temporarily has no default input or its transport cannot be read.
int captureRoutingArbitrationState() {
	AudioObjectID deviceID = queryDefaultDeviceID(kAudioHardwarePropertyDefaultInputDevice);
	if (deviceID == kAudioObjectUnknown) return -1;

	AudioObjectPropertyAddress addr;
	addr.mSelector = kAudioDevicePropertyTransportType;
	addr.mScope = kAudioObjectPropertyScopeGlobal;
	addr.mElement = kAudioObjectPropertyElementMain;
	UInt32 transportType = 0;
	UInt32 size = sizeof(transportType);
	OSStatus err = AudioObjectGetPropertyData(deviceID, &addr, 0, NULL, &size, &transportType);
	if (err != noErr) return -1;
	return transportType == kAudioDeviceTransportTypeBluetooth ||
		transportType == kAudioDeviceTransportTypeBluetoothLE;
}
*/
import "C"
import "sync"

var coreAudioListenerOnce sync.Once

func installCoreAudioListener() {
	coreAudioListenerOnce.Do(func() {
		C.installCADeviceListener()
	})
}

func coreAudioDeviceChanged() bool {
	return C.checkAndResetCADeviceChanged() != 0
}

func captureRoutingArbitrationNeeded() bool {
	return C.captureRoutingArbitrationState() != 0
}
