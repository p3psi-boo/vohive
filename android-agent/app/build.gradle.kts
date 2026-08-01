plugins {
    id("com.android.application")
}

android {
    namespace = "com.vohive.agent"
    compileSdk = 37

    defaultConfig {
        applicationId = "com.vohive.agent"
        minSdk = 26
        targetSdk = 37
        versionCode = 3
        versionName = "0.3.0"
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    lint {
        // Toolchain update notices are handled independently from source lint.
        disable += "AndroidGradlePluginVersion"
    }
}

dependencies {
    implementation("com.squareup.okhttp3:okhttp-android:5.4.0")
}
