#pragma once

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

void* kopi_webrtc_aec_create(int sample_rate, int channels);
void  kopi_webrtc_aec_destroy(void* handle);
int   kopi_webrtc_aec_analyze_render(void* handle, const int16_t* data, int samples);
int   kopi_webrtc_aec_process_capture(void* handle, const int16_t* in, int samples, int16_t* out);
void  kopi_webrtc_aec_reset(void* handle);

void* kopi_webrtc_ns_create(int sample_rate, int channels, int level);
void  kopi_webrtc_ns_destroy(void* handle);
int   kopi_webrtc_ns_process(void* handle, const int16_t* in, int samples, int16_t* out);
int   kopi_webrtc_ns_set_level(void* handle, int level);
void  kopi_webrtc_ns_reset(void* handle);

#ifdef __cplusplus
}
#endif
