#include "kopi_webrtc_aec_bridge.h"

#include <cstdint>
#include <memory>
#include <new>

#include "api/scoped_refptr.h"
#include "modules/audio_processing/include/audio_processing.h"

namespace {

struct KopiWebRTCAEC {
  rtc::scoped_refptr<webrtc::AudioProcessing> apm;
  webrtc::StreamConfig stream_config;
};

struct KopiWebRTCNS {
  rtc::scoped_refptr<webrtc::AudioProcessing> apm;
  webrtc::StreamConfig stream_config;
  int level;
};

// KopiNSLevel 把 Go 侧传入的整数档位映射到 WebRTC APM 的 4 档枚举：
//   1 = kLow / 2 = kModerate (默认) / 3 = kHigh / 4 = kVeryHigh
// 任何越界值都退到 kModerate，避免上层数字传错时变得过激或完全无效。
webrtc::AudioProcessing::Config::NoiseSuppression::Level KopiNSLevel(int level) {
  switch (level) {
    case 1:
      return webrtc::AudioProcessing::Config::NoiseSuppression::Level::kLow;
    case 3:
      return webrtc::AudioProcessing::Config::NoiseSuppression::Level::kHigh;
    case 4:
      return webrtc::AudioProcessing::Config::NoiseSuppression::Level::kVeryHigh;
    case 2:
    default:
      return webrtc::AudioProcessing::Config::NoiseSuppression::Level::kModerate;
  }
}

void ApplyAECConfig(const rtc::scoped_refptr<webrtc::AudioProcessing>& apm) {
  if (!apm) {
    return;
  }
  webrtc::AudioProcessing::Config config;
  config.echo_canceller.enabled = true;
  config.echo_canceller.mobile_mode = false;
  config.gain_controller1.enabled = false;
  config.gain_controller2.enabled = false;
  config.high_pass_filter.enabled = false;
  config.noise_suppression.enabled = false;
  config.transient_suppression.enabled = false;
  apm->ApplyConfig(config);
}

void ApplyNSConfig(const rtc::scoped_refptr<webrtc::AudioProcessing>& apm, int level) {
  if (!apm) {
    return;
  }
  webrtc::AudioProcessing::Config config;
  config.echo_canceller.enabled = false;
  config.gain_controller1.enabled = false;
  config.gain_controller2.enabled = false;
  config.high_pass_filter.enabled = false;
  config.noise_suppression.enabled = level > 0;
  config.noise_suppression.level = KopiNSLevel(level);
  config.transient_suppression.enabled = false;
  apm->ApplyConfig(config);
}

}  // namespace

extern "C" void* kopi_webrtc_aec_create(int sample_rate, int channels) {
  if (sample_rate <= 0 || channels <= 0) {
    return nullptr;
  }

  auto* bridge = new (std::nothrow) KopiWebRTCAEC();
  if (!bridge) {
    return nullptr;
  }

  bridge->apm = webrtc::AudioProcessingBuilder().Create();
  if (!bridge->apm) {
    delete bridge;
    return nullptr;
  }

  ApplyAECConfig(bridge->apm);
  bridge->stream_config = webrtc::StreamConfig(sample_rate, static_cast<size_t>(channels));
  return bridge;
}

extern "C" void kopi_webrtc_aec_destroy(void* handle) {
  auto* bridge = static_cast<KopiWebRTCAEC*>(handle);
  delete bridge;
}

extern "C" int kopi_webrtc_aec_analyze_render(void* handle, const int16_t* data, int samples) {
  auto* bridge = static_cast<KopiWebRTCAEC*>(handle);
  if (!bridge || !bridge->apm || !data || samples <= 0) {
    return -1;
  }
  if (static_cast<size_t>(samples) != bridge->stream_config.num_samples()) {
    return -2;
  }
  return bridge->apm->ProcessReverseStream(
      data,
      bridge->stream_config,
      bridge->stream_config,
      const_cast<int16_t*>(data));
}

extern "C" int kopi_webrtc_aec_process_capture(void* handle, const int16_t* in, int samples, int16_t* out) {
  auto* bridge = static_cast<KopiWebRTCAEC*>(handle);
  if (!bridge || !bridge->apm || !in || !out || samples <= 0) {
    return -1;
  }
  if (static_cast<size_t>(samples) != bridge->stream_config.num_samples()) {
    return -2;
  }
  return bridge->apm->ProcessStream(
      in,
      bridge->stream_config,
      bridge->stream_config,
      out);
}

extern "C" void kopi_webrtc_aec_reset(void* handle) {
  auto* bridge = static_cast<KopiWebRTCAEC*>(handle);
  if (!bridge || !bridge->apm) {
    return;
  }

  bridge->apm = webrtc::AudioProcessingBuilder().Create();
  if (!bridge->apm) {
    return;
  }

  ApplyAECConfig(bridge->apm);
}

extern "C" void* kopi_webrtc_ns_create(int sample_rate, int channels, int level) {
  if (sample_rate <= 0 || channels <= 0) {
    return nullptr;
  }

  auto* bridge = new (std::nothrow) KopiWebRTCNS();
  if (!bridge) {
    return nullptr;
  }

  bridge->apm = webrtc::AudioProcessingBuilder().Create();
  if (!bridge->apm) {
    delete bridge;
    return nullptr;
  }

  bridge->stream_config = webrtc::StreamConfig(sample_rate, static_cast<size_t>(channels));
  bridge->level = level;
  ApplyNSConfig(bridge->apm, level);
  return bridge;
}

extern "C" void kopi_webrtc_ns_destroy(void* handle) {
  auto* bridge = static_cast<KopiWebRTCNS*>(handle);
  delete bridge;
}

extern "C" int kopi_webrtc_ns_process(void* handle, const int16_t* in, int samples, int16_t* out) {
  auto* bridge = static_cast<KopiWebRTCNS*>(handle);
  if (!bridge || !bridge->apm || !in || !out || samples <= 0) {
    return -1;
  }
  if (static_cast<size_t>(samples) != bridge->stream_config.num_samples()) {
    return -2;
  }
  return bridge->apm->ProcessStream(
      in,
      bridge->stream_config,
      bridge->stream_config,
      out);
}

extern "C" int kopi_webrtc_ns_set_level(void* handle, int level) {
  auto* bridge = static_cast<KopiWebRTCNS*>(handle);
  if (!bridge || !bridge->apm) {
    return -1;
  }
  bridge->level = level;
  ApplyNSConfig(bridge->apm, level);
  return 0;
}

extern "C" void kopi_webrtc_ns_reset(void* handle) {
  auto* bridge = static_cast<KopiWebRTCNS*>(handle);
  if (!bridge || !bridge->apm) {
    return;
  }

  bridge->apm = webrtc::AudioProcessingBuilder().Create();
  if (!bridge->apm) {
    return;
  }
  ApplyNSConfig(bridge->apm, bridge->level);
}
