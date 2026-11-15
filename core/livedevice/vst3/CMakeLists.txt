# livedevice: the VST3 proxy every hack installs as "<Name>.vst3"
# (core/livedevice). Built by build.sh in Docker; the result is committed as
# livedevice.so.
cmake_minimum_required(VERSION 3.25)
project(LiveDevice VERSION 0.1.0 LANGUAGES C CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# The SDK (MIT), without its GUI library, examples or symlinks into ~/.vst3.
# Of the hosting examples only Steinberg's validator is built (the others
# need X11, and nothing here has a GUI).
set(SMTG_ENABLE_VST3_PLUGIN_EXAMPLES OFF CACHE BOOL "" FORCE)
set(SMTG_ENABLE_VST3_HOSTING_EXAMPLES OFF CACHE BOOL "" FORCE)
set(SMTG_ENABLE_VSTGUI_SUPPORT OFF CACHE BOOL "" FORCE)
set(SMTG_CREATE_PLUGIN_LINK OFF CACHE BOOL "" FORCE)
set(SMTG_RUN_VST_VALIDATOR OFF CACHE BOOL "" FORCE)
# No moduleinfo.json: the classes come from the hack's class file at load
# time, so a file written at build time would list none, and a host that
# trusts it would see no device at all.
set(SMTG_CREATE_MODULE_INFO OFF CACHE BOOL "" FORCE)
set(VST3SDK ${CMAKE_CURRENT_SOURCE_DIR}/.sdk CACHE PATH "the VST3 SDK (build.sh fetches it)")
add_subdirectory(${VST3SDK} ${PROJECT_BINARY_DIR}/vst3sdk)
smtg_enable_vst3_sdk()

smtg_add_vst3plugin(livedevice
    source/proxy.cpp
    source/params.cpp
)
target_link_libraries(livedevice PRIVATE sdk pthread dl)

# playtest: hosts a bundle like Live and checks real sound through its hack
add_executable(playtest test/playtest.cpp
    ${VST3SDK}/public.sdk/source/vst/hosting/module.cpp
    ${VST3SDK}/public.sdk/source/vst/hosting/module_linux.cpp
    ${VST3SDK}/public.sdk/source/vst/hosting/plugprovider.cpp
    ${VST3SDK}/public.sdk/source/vst/hosting/connectionproxy.cpp
    ${VST3SDK}/public.sdk/source/vst/moduleinfo/moduleinfoparser.cpp
    ${VST3SDK}/public.sdk/source/common/memorystream.cpp)
target_link_libraries(playtest PRIVATE sdk_hosting)
