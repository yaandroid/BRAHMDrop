plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "com.brahm.drop"
    compileSdk = 34

    defaultConfig {
        applicationId = "com.brahm.drop"
        minSdk = 24
        targetSdk = 34
        versionCode = 1          // kept fixed pre-release (do not bump)
        versionName = "1.1"      // visible version, fixed
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
        }
    }

    // The Go server binary ships as a native library per ABI.
    // Do not compress it so it can be exec'd from nativeLibraryDir.
    packaging {
        jniLibs {
            useLegacyPackaging = true
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.appcompat:appcompat:1.7.0")
}
